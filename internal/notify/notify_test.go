package notify

import "testing"

func TestQuoteAS(t *testing.T) {
	got := quoteAS(`CI done: "fail" \ path`)
	want := `"CI done: \"fail\" \\ path"`
	if got != want {
		t.Fatalf("got %s want %s", got, want)
	}
}
