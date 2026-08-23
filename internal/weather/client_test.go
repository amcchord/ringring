package weather

import (
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGeocodeAndCurrent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/geo":
			if r.URL.Query().Get("name") != "Portland, Maine" {
				t.Fatalf("unexpected geocoding query: %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"results":[{"name":"Portland","admin1":"Maine","country":"United States","latitude":43.66,"longitude":-70.25}]}`))
		case "/forecast":
			if !strings.Contains(r.URL.Query().Get("current"), "weather_code") || r.URL.Query().Get("temperature_unit") != "fahrenheit" {
				t.Fatalf("unexpected forecast query: %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"current":{"temperature_2m":72.4,"apparent_temperature":71.1,"weather_code":2},"daily":{"temperature_2m_max":[76.2],"temperature_2m_min":[59.8],"precipitation_probability_max":[20]}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := New(server.Client())
	client.geocodingURL = server.URL + "/geo"
	client.forecastURL = server.URL + "/forecast"
	location, err := client.Geocode(t.Context(), " Portland,   Maine ")
	if err != nil {
		t.Fatal(err)
	}
	if location.Label != "Portland, Maine" || location.Latitude != 43.66 {
		t.Fatalf("unexpected location: %#v", location)
	}
	conditions, err := client.Current(t.Context(), location.Latitude, location.Longitude)
	if err != nil {
		t.Fatal(err)
	}
	if conditions.Temperature != 72.4 || conditions.High != 76.2 || Description(conditions.WeatherCode) != "partly cloudy skies" {
		t.Fatalf("unexpected conditions: %#v", conditions)
	}
}

func TestCurrentRejectsNonFiniteCoordinates(t *testing.T) {
	client := New(nil)
	for _, coordinates := range [][2]float64{{math.NaN(), 0}, {0, math.Inf(1)}} {
		if _, err := client.Current(t.Context(), coordinates[0], coordinates[1]); err == nil {
			t.Fatalf("non-finite coordinates were accepted: %#v", coordinates)
		}
	}
}
