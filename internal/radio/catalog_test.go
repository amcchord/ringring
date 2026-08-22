package radio

import (
	"net/url"
	"regexp"
	"strings"
	"testing"
)

func TestCatalogIsFixedAndDialplanSafe(t *testing.T) {
	stations := All()
	if len(stations) != 3 {
		t.Fatalf("station count = %d", len(stations))
	}
	identifier := regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
	seen := make(map[string]bool)
	for _, station := range stations {
		if !identifier.MatchString(station.ID) || seen[station.ID] {
			t.Fatalf("unsafe or duplicate station ID: %q", station.ID)
		}
		seen[station.ID] = true
		if station.Name == "" || station.Description == "" || strings.ContainsAny(station.Name+station.Description, "\r\n\t") {
			t.Fatalf("unsafe station copy: %#v", station)
		}
		stream, err := url.Parse(station.StreamURL)
		if err != nil || stream.Scheme != "http" || stream.Host != "ice5.somafm.com" || !strings.HasSuffix(stream.Path, "-128-mp3") || stream.RawQuery != "" || stream.Fragment != "" {
			t.Fatalf("unsafe stream URL: %q", station.StreamURL)
		}
		source, err := url.Parse(station.SourceURL)
		if err != nil || source.Scheme != "https" || source.Host != "somafm.com" || !strings.HasSuffix(source.Path, "/directstreamlinks.html") {
			t.Fatalf("unsafe source URL: %q", station.SourceURL)
		}
		resolved, ok := Lookup(station.ID)
		if !ok || resolved != station {
			t.Fatalf("station did not round trip: %#v", station)
		}
	}
	if !seen[DefaultStationID] {
		t.Fatal("default station is absent")
	}
	if station, ok := Resolve(""); !ok || station.ID != DefaultStationID {
		t.Fatalf("empty legacy station did not resolve to default: %#v %t", station, ok)
	}
	if _, ok := Resolve("http://example.test/stream"); ok {
		t.Fatal("arbitrary URL resolved as a station")
	}
}

func TestAllReturnsACopy(t *testing.T) {
	first := All()
	first[0].Name = "changed"
	if All()[0].Name == "changed" {
		t.Fatal("caller changed the shared catalog")
	}
}
