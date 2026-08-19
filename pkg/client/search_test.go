package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSearchBuilderRepeatedAndOrParams(t *testing.T) {
	c, err := New(Config{BaseURL: "http://example.com"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	sb := c.SearchBuilder("Patient").
		Param("family", "Smith").
		AddParam("identifier", "a").
		AddParam("identifier", "b").
		OrParam("gender", "male", "female").
		Count(50).
		Sort("family", "-birthdate")

	values := sb.Values()
	if values.Get("family") != "Smith" {
		t.Fatalf("family: %s", values.Get("family"))
	}
	if len(values["identifier"]) != 2 {
		t.Fatalf("identifier: %v", values["identifier"])
	}
	if values.Get("gender") != "male,female" {
		t.Fatalf("gender: %s", values.Get("gender"))
	}
	if values.Get("_count") != "50" {
		t.Fatalf("_count: %s", values.Get("_count"))
	}
	if values.Get("_sort") != "family,-birthdate" {
		t.Fatalf("_sort: %s", values.Get("_sort"))
	}
}

func TestSearchPaginationLinkFollowing(t *testing.T) {
	page := 1
	srv := startSearchServer(t, &page)
	defer srv.Close()

	c, _ := New(Config{BaseURL: srv.URL})
	all, err := c.SearchAll(context.Background(), "Patient", map[string]string{"family": "Smith"})
	if err != nil {
		t.Fatalf("SearchAll: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("expected 3 patients, got %d", len(all))
	}
}

func TestSearchPageRelativeURL(t *testing.T) {
	page := 1
	srv := startSearchServer(t, &page)
	defer srv.Close()
	c, _ := New(Config{BaseURL: srv.URL})

	first, err := c.Search(context.Background(), "Patient", nil)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if !first.HasNext() {
		t.Fatal("expected next page")
	}
	second, err := c.SearchPage(context.Background(), "Patient", first.NextURL)
	if err != nil {
		t.Fatalf("SearchPage: %v", err)
	}
	if len(second.Entries) == 0 {
		t.Fatal("expected entries on second page")
	}
}

func TestSearchIteratorAdvancesOnlyOnNext(t *testing.T) {
	page := 1
	srv := startSearchServer(t, &page)
	defer srv.Close()
	c, _ := New(Config{BaseURL: srv.URL})
	it := c.IterateSearch(context.Background(), "Patient", nil)
	if !it.Next() || it.Resource().ID != "p1" {
		t.Fatal("expected first iterator resource")
	}
	if got := it.Resource(); got == nil || got.ID != "p1" {
		t.Fatalf("Resource should remain the current item, got %#v", got)
	}
	if !it.Next() || it.Resource().ID != "p2" {
		t.Fatal("expected second iterator resource")
	}
	if !it.Next() || it.Resource().ID != "p3" {
		t.Fatal("expected resource from next page")
	}
	next := it.Next()
	if next || it.Err() != nil {
		t.Fatalf("expected clean iterator exhaustion, next=%v err=%v", next, it.Err())
	}
}

func startSearchServer(t *testing.T, page *int) *httptest.Server {
	t.Helper()
	*page = 1
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("page") {
		case "2":
			_, _ = w.Write([]byte(`{"resourceType":"Bundle","type":"searchset","entry":[{"resource":{"resourceType":"Patient","id":"p3"}}]}`))
		case "3":
			_, _ = w.Write([]byte(`{"resourceType":"Bundle","type":"searchset","entry":[]}`))
		default:
			_, _ = w.Write([]byte(`{"resourceType":"Bundle","type":"searchset","link":[{"relation":"next","url":"/fhir/Patient?page=2"}],"entry":[{"resource":{"resourceType":"Patient","id":"p1"}},{"resource":{"resourceType":"Patient","id":"p2"}}]}`))
		}
	}))
}
