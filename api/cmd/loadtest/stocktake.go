package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/ADEXITUM/pikni/api/internal/seed"
)

// stocktake — федеральная ревизия: каждый сотрудник из плана (pikni-admin seed-load) сканирует
// свои марки в своей зоне с паузой, как человек с телефоном, — около скана в 1,5 секунды. Нагрузка
// нарастает за ramp, замер — после разгона.
func stocktake(base, planFile string, duration, ramp, think time.Duration, limit int) {
	data, err := os.ReadFile(planFile)
	if err != nil {
		fmt.Fprintln(os.Stderr, "loadtest:", err)
		os.Exit(1)
	}
	var plan seed.LoadPlan
	if err := json.Unmarshal(data, &plan); err != nil {
		fmt.Fprintln(os.Stderr, "loadtest: plan:", err)
		os.Exit(1)
	}
	scanners := plan.Scanners
	if limit > 0 && limit < len(scanners) {
		scanners = scanners[:limit]
	}
	client := &http.Client{Timeout: 30 * time.Second, Transport: &http.Transport{
		MaxIdleConns: len(scanners) + 100, MaxIdleConnsPerHost: len(scanners) + 100, IdleConnTimeout: time.Minute,
	}}
	start := time.Now()
	measureFrom := start.Add(ramp)
	deadline := measureFrom.Add(duration)
	var mu sync.Mutex
	var samples []sample
	var inFlight, peak atomic.Int64
	statuses := map[int]int{}
	var wg sync.WaitGroup
	for k, sc := range scanners {
		if len(sc.Codes) == 0 {
			continue
		}
		wg.Add(1)
		go func(k int, sc seed.Scanner) {
			defer wg.Done()
			// Сотрудники подключаются равномерно за время разгона.
			time.Sleep(time.Duration(float64(ramp) * float64(k) / float64(len(scanners))))
			for n := 0; time.Now().Before(deadline); n++ {
				body, _ := json.Marshal(map[string]any{
					"id": uuid.NewString(), "raw": sc.Codes[n%len(sc.Codes)], "source": "camera",
					"scannedAt": time.Now().UTC().Format(time.RFC3339Nano), "zoneId": sc.Zone,
				})
				req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost,
					base+"/api/v1/documents/"+sc.Doc+"/scans", bytes.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Cookie", "pikni_sid="+sc.Token)
				req.Header.Set("X-CSRF-Token", sc.CSRF)
				t := time.Now()
				cur := inFlight.Add(1)
				for p := peak.Load(); cur > p && !peak.CompareAndSwap(p, cur); p = peak.Load() {
				}
				resp, err := client.Do(req)
				inFlight.Add(-1)
				code := 0
				if err == nil {
					code = resp.StatusCode
					_ = resp.Body.Close()
				}
				if t.After(measureFrom) {
					mu.Lock()
					samples = append(samples, sample{"stocktake-scan", time.Since(t), err == nil && code < 400})
					statuses[code]++
					mu.Unlock()
				}
				time.Sleep(time.Duration(float64(think) * (0.5 + rand.Float64())))
			}
		}(k, sc)
	}
	wg.Wait()
	fmt.Printf("Федеральная ревизия: сотрудников %d, разгон %s, пауза между сканами ~%s\n", len(scanners), ramp, think)
	report(samples, len(scanners), duration)
	fmt.Printf("Одновременно в обработке (пик): %d; ответы: %v (0 — нет ответа или таймаут)\n", peak.Load(), statuses)
}
