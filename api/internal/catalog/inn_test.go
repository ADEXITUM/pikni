package catalog

import "testing"

func TestINNValid(t *testing.T) {
	t.Parallel()
	for inn, want := range map[string]bool{
		"7707083893":   true,
		"7707083894":   false,
		"500100732259": true,
		"500100732258": false,
		"500100732269": false,
		"770708389":    false,
		"77070838931a": false,
	} {
		if got := innValid(inn); got != want {
			t.Errorf("innValid(%s) = %v, want %v", inn, got, want)
		}
	}
}
