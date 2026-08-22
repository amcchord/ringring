// Package radio owns the fixed station catalog that hosts may choose from.
// Stream addresses never come from a request or the database.
package radio

const DefaultStationID = "groove-salad"

type Station struct {
	ID          string
	Name        string
	Description string
	StreamURL   string
	SourceURL   string
}

var catalog = []Station{
	{
		ID:          DefaultStationID,
		Name:        "Groove Salad",
		Description: "Chilled ambient and downtempo beats",
		StreamURL:   "http://ice5.somafm.com/groovesalad-128-mp3",
		SourceURL:   "https://somafm.com/groovesalad/directstreamlinks.html",
	},
	{
		ID:          "drone-zone",
		Name:        "Drone Zone",
		Description: "Slow atmospheric textures with minimal beats",
		StreamURL:   "http://ice5.somafm.com/dronezone-128-mp3",
		SourceURL:   "https://somafm.com/dronezone/directstreamlinks.html",
	},
	{
		ID:          "deep-space-one",
		Name:        "Deep Space One",
		Description: "Deep ambient and space music",
		StreamURL:   "http://ice5.somafm.com/deepspaceone-128-mp3",
		SourceURL:   "https://somafm.com/deepspaceone/directstreamlinks.html",
	},
}

func All() []Station {
	return append([]Station(nil), catalog...)
}

func Lookup(id string) (Station, bool) {
	for _, station := range catalog {
		if station.ID == id {
			return station, true
		}
	}
	return Station{}, false
}

// Resolve keeps legacy in-memory routing values compatible while rejecting
// every nonempty identifier outside the catalog.
func Resolve(id string) (Station, bool) {
	if id == "" {
		id = DefaultStationID
	}
	return Lookup(id)
}
