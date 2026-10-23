package catalog

import "regexp"

var innPattern = regexp.MustCompile(`^([0-9]{10}|[0-9]{12})$`)

// innValid проверяет контрольные цифры ИНН: у организации (10 цифр) одна, у предпринимателя
// (12 цифр) две. Каждая — сумма цифр с весами по остатку от деления на 11, затем на 10.
func innValid(inn string) bool {
	if !innPattern.MatchString(inn) {
		return false
	}
	check := func(weights []int) bool {
		sum := 0
		for i, w := range weights {
			sum += w * int(inn[i]-'0')
		}
		return sum%11%10 == int(inn[len(weights)]-'0')
	}
	if len(inn) == 10 {
		return check([]int{2, 4, 10, 3, 5, 9, 4, 6, 8})
	}
	return check([]int{7, 2, 4, 10, 3, 5, 9, 4, 6, 8}) && check([]int{3, 7, 2, 4, 10, 3, 5, 9, 4, 6, 8})
}
