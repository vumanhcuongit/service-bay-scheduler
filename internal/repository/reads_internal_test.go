package repository

import "testing"

// GetDealership runs on every booking, so the timezone is loaded once per name and shared.
func TestLocationCached(t *testing.T) {
	first, err := location("Asia/Ho_Chi_Minh")
	if err != nil {
		t.Fatal(err)
	}
	second, err := location("Asia/Ho_Chi_Minh")
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Errorf("location returned %p then %p, want the same *time.Location", first, second)
	}
}
