package store

import (
	"context"
	"fmt"
)

// ErrLocationOwnedByAnotherOrg is returned by SetLocation when the location id is
// already used by a location of another org: location ids are shared by all orgs. It
// wraps ErrAlreadyExists.
var ErrLocationOwnedByAnotherOrg = fmt.Errorf("location id belongs to another org: %w", ErrAlreadyExists)

type GeoLocation struct {
	Latitude  string
	Longitude string
}

type Connector struct {
	Format      string
	Id          string
	MaxAmperage int32
	MaxVoltage  int32
	PowerType   string
	Standard    string
	LastUpdated string
}

type Evse struct {
	Connectors  []Connector
	EvseId      *string
	Status      string
	Uid         string
	LastUpdated string
}

type Location struct {
	Address     string
	City        string
	Coordinates GeoLocation
	Country     string
	Evses       *[]Evse
	Id          string
	LastUpdated string
	Name        string
	ParkingType string
	PostalCode  string
	// OrgId is the org (UUID) that owns the location. It is required by SetLocation
	// (PostgreSQL and in-memory), which refuses to replace a location of another org
	// with an error wrapping ErrLocationOwnedByAnotherOrg: location ids are global. The
	// org is a column of its own, not part of the stored location document. Firestore
	// does not store it: it always reads as "".
	OrgId string `json:"-" firestore:"-"`
}

type LocationStore interface {
	SetLocation(ctx context.Context, location *Location) error
	LookupLocation(ctx context.Context, locationId string) (*Location, error)
	ListLocations(context context.Context, offset int, limit int) ([]*Location, error)
}
