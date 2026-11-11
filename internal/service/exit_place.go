package service

import (
	"math"

	"hop/internal/model"
)

// LocalPlace answers where the user is, for the home's map.
type LocalPlace interface {
	Home() (model.GeoPoint, bool, error)
}

// PlaceService answers where the system's time zone is.
type PlaceService struct {
	place LocalPlace
}

// NewPlaceService returns a service reading through place.
func NewPlaceService(place LocalPlace) *PlaceService {
	return &PlaceService{place: place}
}

// Home returns where the system's time zone is, and false when it is not known.
func (s *PlaceService) Home() (model.GeoPoint, bool, error) {
	return s.place.Home()
}

// ExitPlace is where the route's exit is. A country the exit's address was
// measured in (measuredCountry, "" when not known for this exit) wins over
// the flag in the node's name; a direct route leaves where the user is; an
// exit that neither tells is unknown.
func ExitPlace(route []model.RouteHop, measuredCountry string) model.ExitPlace {
	if len(route) == 0 {
		return model.ExitPlace{}
	}
	hop := route[len(route)-1]
	if hop.Node.PlanNote || hop.Node.Via != "" {
		return model.ExitPlace{Kind: model.ExitPlaceDirect}
	}
	if point, ok := CountryPoint(measuredCountry); ok {
		return model.ExitPlace{Kind: model.ExitPlaceMeasured, Country: measuredCountry, Point: point}
	}
	if hop.Group {
		return model.ExitPlace{}
	}
	if code, ok := FlagCountry(hop.Node.Flag); ok {
		if point, ok := CountryPoint(code); ok {
			return model.ExitPlace{Kind: model.ExitPlaceByName, Country: code, Point: point}
		}
	}
	return model.ExitPlace{}
}

// earthRadiusKm is the earth's mean radius.
const earthRadiusKm = 6371

// DistanceKm is how far apart two places are over the earth's surface, in
// kilometres.
func DistanceKm(a model.GeoPoint, b model.GeoPoint) float64 {
	rad := func(deg float32) float64 { return float64(deg) * math.Pi / 180 }
	dLat, dLon := rad(b.Lat-a.Lat), rad(b.Lon-a.Lon)
	h := math.Sin(dLat/2)*math.Sin(dLat/2) + math.Cos(rad(a.Lat))*math.Cos(rad(b.Lat))*math.Sin(dLon/2)*math.Sin(dLon/2)
	return 2 * earthRadiusKm * math.Asin(math.Sqrt(h))
}
