package ui

import (
	"reflect"
	"testing"

	"github.com/esobczak1970/chrome-optimizer/browser"
)

func TestFilterInfosMatchesTitleURLDomainAndID(t *testing.T) {
	input := []browser.TabInfo{
		{Tab: browser.Tab{TargetID: "A123", Domain: "example.com", Title: "Hello world", URL: "https://example.com"}},
		{Tab: browser.Tab{TargetID: "B456", Domain: "news.test", Title: "Latest News", URL: "https://news.test/article/42"}},
	}
	for _, test := range []struct {
		query string
		want  []string
	}{
		{"", []string{"A123", "B456"}},
		{"  ExAmPlE  ", []string{"A123"}},
		{"hello", []string{"A123"}},
		{"ARTICLE/42", []string{"B456"}},
		{"b456", []string{"B456"}},
		{"missing", nil},
	} {
		matched := FilterInfos(input, test.query)
		var ids []string
		for _, item := range matched {
			ids = append(ids, item.TargetID)
		}
		if !reflect.DeepEqual(ids, test.want) {
			t.Fatalf("FilterInfos(%q) = %v, want %v", test.query, ids, test.want)
		}
	}
	if len(input) != 2 || input[0].TargetID != "A123" || input[1].TargetID != "B456" {
		t.Fatal("filter mutated caller's snapshot")
	}
}
