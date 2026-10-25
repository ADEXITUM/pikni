// Command loadtest — нагрузочное испытание (П-33, п. 4.4.2): виртуальные пользователи и кассы
// параллельно выполняют типичные запросы чтения, в конце — процентили времени ответа по каждому.
//
//	go run ./cmd/loadtest -url http://localhost:8080 -login director -password ... -users 80 -tills 20 -d 60s
//
// Федеральная ревизия (решение 0063): сотрудники из плана pikni-admin seed-load сканируют марки.
//
//	go run ./cmd/loadtest -url http://localhost:8080 -plan load.json -d 3m -ramp 2m
//
// Вход выполняется один раз: ограничение 10 попыток в минуту с адреса не даёт сотне пользователей
// войти разом, поэтому виртуальные пользователи работают в одном сеансе — для сервера это та же
// сотня параллельных запросов.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"math/rand/v2"
	"net/http"
	"os"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
)

type sample struct {
	name string
	dur  time.Duration
	ok   bool
}

type request struct {
	name, method, path string
	body               any
	weight             int
}

func main() {
	base := flag.String("url", "http://localhost:8080", "адрес API")
	login := flag.String("login", "director", "логин сотрудника")
	password := flag.String("password", "", "пароль")
	users := flag.Int("users", 80, "виртуальных пользователей")
	tills := flag.Int("tills", 20, "виртуальных касс")
	posKey := flag.String("pos-key", "", "ключ кассы; без него кассы читают каталог через API сотрудника")
	duration := flag.Duration("d", 60*time.Second, "длительность")
	planFile := flag.String("plan", "", "план федеральной ревизии (pikni-admin seed-load)")
	ramp := flag.Duration("ramp", time.Minute, "разгон: за это время подключаются все сотрудники плана")
	think := flag.Duration("think", 1500*time.Millisecond, "пауза сотрудника между сканами")
	limit := flag.Int("scanners", 0, "сколько сотрудников плана задействовать; 0 — всех")
	flag.Parse()
	if *planFile != "" {
		stocktake(*base, *planFile, *duration, *ramp, *think, *limit)
		return
	}

	cookie, csrf, err := signIn(*base, *login, *password)
	if err != nil {
		fmt.Fprintln(os.Stderr, "loadtest:", err)
		os.Exit(1)
	}
	locations, err := locationIDs(*base, cookie)
	if err != nil || len(locations) == 0 {
		fmt.Fprintln(os.Stderr, "loadtest: locations:", err)
		os.Exit(1)
	}
	words := []string{"водка", "пиво", "вино", "коньяк", "тайга", "амурское", "виски", "сидр"}
	staff := []request{
		{"availability", "GET", "/api/v1/availability?q=%s", nil, 30},
		{"products", "GET", "/api/v1/products?q=%s", nil, 20},
		{"documents", "GET", "/api/v1/documents", nil, 15},
		{"stock", "GET", "/api/v1/stock/balances?locationId=@loc", nil, 10},
		{"me", "GET", "/api/v1/auth/me", nil, 10},
		{"notifications", "GET", "/api/v1/notifications", nil, 10},
		{"report", "POST", "/api/v1/reports/stock_on_date", map[string]any{}, 5},
	}
	till := []request{{"pos-catalog", "GET", "/api/pos/v1/catalog", nil, 1}}
	if *posKey == "" {
		till = []request{{"pos-catalog(staff)", "GET", "/api/v1/products?q=%s", nil, 1}}
	}

	client := &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{MaxIdleConnsPerHost: 200}}
	var mu sync.Mutex
	var samples []sample
	deadline := time.Now().Add(*duration)
	var wg sync.WaitGroup
	run := func(mix []request, auth func(*http.Request)) {
		defer wg.Done()
		total := 0
		for _, r := range mix {
			total += r.weight
		}
		for time.Now().Before(deadline) {
			pick := rand.IntN(total)
			r := mix[0]
			for _, c := range mix {
				if pick < c.weight {
					r = c
					break
				}
				pick -= c.weight
			}
			path := r.path
			if strings.Contains(path, "%s") {
				path = fmt.Sprintf(path, words[rand.IntN(len(words))])
			}
			path = strings.ReplaceAll(path, "@loc", locations[rand.IntN(len(locations))])
			var body []byte
			if r.body != nil {
				body, _ = json.Marshal(r.body)
			}
			req, _ := http.NewRequestWithContext(context.Background(), r.method, *base+path, bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			auth(req)
			start := time.Now()
			resp, err := client.Do(req)
			ok := err == nil && resp.StatusCode < 400
			if err == nil {
				_ = resp.Body.Close()
			}
			mu.Lock()
			samples = append(samples, sample{r.name, time.Since(start), ok})
			mu.Unlock()
			// Человек и касса не шлют запросы непрерывно: пауза 0,5–1,5 с между действиями.
			time.Sleep(time.Duration(500+rand.IntN(1000)) * time.Millisecond)
		}
	}
	staffAuth := func(r *http.Request) {
		r.Header.Set("Cookie", cookie)
		r.Header.Set("X-CSRF-Token", csrf)
	}
	tillAuth := staffAuth
	if *posKey != "" {
		tillAuth = func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+*posKey) }
	}
	for range *users {
		wg.Add(1)
		go run(staff, staffAuth)
	}
	for range *tills {
		wg.Add(1)
		go run(till, tillAuth)
	}
	wg.Wait()
	report(samples, *users+*tills, *duration)
}

func signIn(base, login, password string) (string, string, error) {
	body, _ := json.Marshal(map[string]string{"login": login, "password": password})
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, base+"/api/v1/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("sign in: status %d (нужен сотрудник без двухфакторной аутентификации)", resp.StatusCode)
	}
	var me struct{ CsrfToken string }
	if err := json.NewDecoder(resp.Body).Decode(&me); err != nil {
		return "", "", err
	}
	var cookie string
	for _, c := range resp.Cookies() {
		if c.Name == "pikni_sid" {
			cookie = c.Name + "=" + c.Value
		}
	}
	return cookie, me.CsrfToken, nil
}

func percentile(d []time.Duration, p float64) time.Duration {
	if len(d) == 0 {
		return 0
	}
	return d[min(len(d)-1, int(float64(len(d))*p))]
}

func report(samples []sample, vus int, dur time.Duration) {
	by := map[string][]time.Duration{}
	errs := map[string]int{}
	var all []time.Duration
	failed := 0
	for _, s := range samples {
		by[s.name] = append(by[s.name], s.dur)
		all = append(all, s.dur)
		if !s.ok {
			errs[s.name]++
			failed++
		}
	}
	names := make([]string, 0, len(by))
	for n := range by {
		names = append(names, n)
	}
	sort.Strings(names)
	fmt.Printf("Виртуальных пользователей и касс: %d, длительность %s, запросов %d (%.1f в секунду)\n\n",
		vus, dur, len(samples), float64(len(samples))/dur.Seconds())
	fmt.Printf("%-20s %8s %8s %8s %8s %8s\n", "Запрос", "Кол-во", "p50", "p95", "p99", "Ошибки")
	for _, n := range append(names, "ВСЕГО") {
		d := all
		e := failed
		if n != "ВСЕГО" {
			d, e = by[n], errs[n]
		}
		slices.Sort(d)
		fmt.Printf("%-20s %8d %8s %8s %8s %8d\n", n, len(d), percentile(d, 0.5).Round(time.Millisecond),
			percentile(d, 0.95).Round(time.Millisecond), percentile(d, 0.99).Round(time.Millisecond), e)
	}
	fmt.Printf("\nДоля ошибок: %.2f %%\n", float64(failed)/float64(max(len(samples), 1))*100)
}

func locationIDs(base, cookie string) ([]string, error) {
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, base+"/api/v1/locations", nil)
	req.Header.Set("Cookie", cookie)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	var list struct{ Items []struct{ ID string } }
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return nil, err
	}
	ids := make([]string, len(list.Items))
	for i, l := range list.Items {
		ids[i] = l.ID
	}
	return ids, nil
}
