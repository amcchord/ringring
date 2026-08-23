package weather

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	defaultGeocodingURL = "https://geocoding-api.open-meteo.com/v1/search"
	defaultForecastURL  = "https://api.open-meteo.com/v1/forecast"
)

type Client struct {
	httpClient   *http.Client
	geocodingURL string
	forecastURL  string
}

type Location struct {
	Query     string
	Label     string
	Latitude  float64
	Longitude float64
}

type Conditions struct {
	Temperature         float64
	ApparentTemperature float64
	High                float64
	Low                 float64
	PrecipitationChance int
	WeatherCode         int
}

func New(httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	return &Client{httpClient: httpClient, geocodingURL: defaultGeocodingURL, forecastURL: defaultForecastURL}
}

func (c *Client) Geocode(ctx context.Context, query string) (Location, error) {
	query = strings.Join(strings.Fields(query), " ")
	if len(query) < 2 || len(query) > 80 {
		return Location{}, errors.New("weather location must be 2 to 80 characters")
	}
	values := url.Values{"name": {query}, "count": {"1"}, "language": {"en"}, "format": {"json"}}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.geocodingURL+"?"+values.Encode(), nil)
	if err != nil {
		return Location{}, fmt.Errorf("create geocoding request: %w", err)
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		return Location{}, fmt.Errorf("look up weather location: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return Location{}, fmt.Errorf("weather location service returned %s", response.Status)
	}
	var payload struct {
		Results []struct {
			Name      string  `json:"name"`
			Admin1    string  `json:"admin1"`
			Country   string  `json:"country"`
			Latitude  float64 `json:"latitude"`
			Longitude float64 `json:"longitude"`
		} `json:"results"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&payload); err != nil {
		return Location{}, fmt.Errorf("decode weather location: %w", err)
	}
	if len(payload.Results) == 0 {
		return Location{}, errors.New("weather location was not found")
	}
	result := payload.Results[0]
	if math.IsNaN(result.Latitude) || math.IsInf(result.Latitude, 0) || math.IsNaN(result.Longitude) || math.IsInf(result.Longitude, 0) ||
		result.Latitude < -90 || result.Latitude > 90 || result.Longitude < -180 || result.Longitude > 180 {
		return Location{}, errors.New("weather location returned invalid coordinates")
	}
	parts := []string{result.Name}
	if result.Admin1 != "" && !strings.EqualFold(result.Admin1, result.Name) {
		parts = append(parts, result.Admin1)
	} else if result.Country != "" && !strings.EqualFold(result.Country, result.Name) {
		parts = append(parts, result.Country)
	}
	return Location{Query: query, Label: strings.Join(parts, ", "), Latitude: result.Latitude, Longitude: result.Longitude}, nil
}

func (c *Client) Current(ctx context.Context, latitude, longitude float64) (Conditions, error) {
	if math.IsNaN(latitude) || math.IsInf(latitude, 0) || math.IsNaN(longitude) || math.IsInf(longitude, 0) ||
		latitude < -90 || latitude > 90 || longitude < -180 || longitude > 180 {
		return Conditions{}, errors.New("weather coordinates are invalid")
	}
	values := url.Values{
		"latitude":         {strconv.FormatFloat(latitude, 'f', 5, 64)},
		"longitude":        {strconv.FormatFloat(longitude, 'f', 5, 64)},
		"current":          {"temperature_2m,apparent_temperature,weather_code"},
		"daily":            {"temperature_2m_max,temperature_2m_min,precipitation_probability_max"},
		"temperature_unit": {"fahrenheit"}, "forecast_days": {"1"}, "timezone": {"auto"},
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.forecastURL+"?"+values.Encode(), nil)
	if err != nil {
		return Conditions{}, fmt.Errorf("create forecast request: %w", err)
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		return Conditions{}, fmt.Errorf("get current weather: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return Conditions{}, fmt.Errorf("weather forecast service returned %s", response.Status)
	}
	var payload struct {
		Current struct {
			Temperature float64 `json:"temperature_2m"`
			Apparent    float64 `json:"apparent_temperature"`
			Code        int     `json:"weather_code"`
		} `json:"current"`
		Daily struct {
			High   []float64 `json:"temperature_2m_max"`
			Low    []float64 `json:"temperature_2m_min"`
			Precip []int     `json:"precipitation_probability_max"`
		} `json:"daily"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&payload); err != nil {
		return Conditions{}, fmt.Errorf("decode current weather: %w", err)
	}
	if len(payload.Daily.High) != 1 || len(payload.Daily.Low) != 1 || len(payload.Daily.Precip) != 1 {
		return Conditions{}, errors.New("weather forecast omitted daily values")
	}
	return Conditions{
		Temperature: payload.Current.Temperature, ApparentTemperature: payload.Current.Apparent,
		High: payload.Daily.High[0], Low: payload.Daily.Low[0],
		PrecipitationChance: payload.Daily.Precip[0], WeatherCode: payload.Current.Code,
	}, nil
}

func Description(code int) string {
	switch {
	case code == 0:
		return "clear skies"
	case code == 1:
		return "mostly clear skies"
	case code == 2:
		return "partly cloudy skies"
	case code == 3:
		return "cloudy skies"
	case code == 45 || code == 48:
		return "fog"
	case code >= 51 && code <= 57:
		return "drizzle"
	case code >= 61 && code <= 67:
		return "rain"
	case code >= 71 && code <= 77:
		return "snow"
	case code >= 80 && code <= 82:
		return "rain showers"
	case code == 85 || code == 86:
		return "snow showers"
	case code >= 95:
		return "thunderstorms"
	default:
		return "changing weather"
	}
}
