package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type speciesSeed struct {
	name            string
	dailyPriceCents int
	retireAfterDays int
}

// seedSpecies is the starting catalogue. retireAfterDays is used from schema version 2.
var seedSpecies = []speciesSeed{
	{"Goldfish", 450, 900},
	{"Betta", 600, 540},
	{"Neon Tetra", 250, 400},
	{"Guppy", 200, 365},
	{"Angelfish", 900, 1100},
	{"Clownfish", 1200, 1300},
	{"Discus", 1800, 1200},
	{"Koi", 2500, 2000},
}

var seedTanks = []struct {
	name     string
	location string
	capacity int
	tempC    float64
}{
	{"Harbour Wall", "Front of house", 14, 21.5},
	{"Kelp Room", "Front of house", 12, 24.0},
	{"Reef Annex", "East wing", 12, 25.5},
	{"Lantern Tank", "East wing", 14, 26.0},
	{"Tide Pool", "Back room", 12, 22.0},
	{"Quiet Corner", "Back room", 16, 23.5},
}

var seedFishNames = []string{
	"Admiral", "Biscuit", "Captain", "Dory", "Ember", "Fathom", "Gill", "Harbour", "Inky",
	"Jetsam", "Kipper", "Lagoon", "Marble", "Nemo", "Otter", "Pebble", "Quill", "Ripple",
	"Sprat", "Tiller", "Umber", "Velvet", "Wavelet", "Xebec", "Yawl", "Zephyr", "Anchor",
	"Bubbles", "Comet", "Drift", "Eddy", "Flotsam", "Ginger", "Halyard", "Isla", "Jib",
	"Keel", "Lumen", "Mizzen", "Nori", "Opal", "Puddle", "Quay", "Rudder", "Saffron",
	"Tango", "Urchin", "Vesper", "Whisker", "Yonder", "Brine", "Cobble", "Dapple", "Fennel",
	"Gusto", "Hopper", "Juniper", "Minnow", "Pepper", "Skipper",
}

var seedCustomers = []string{
	"Ada Brine", "Otis Wharf", "Mabel Current", "Rufus Tidewell", "Ines Saltmarsh",
	"Cyrus Lockgate", "Wren Halibut", "Dexter Moorings", "Pearl Eddystone", "Hugo Sandbar",
	"Nell Driftwood", "Felix Quayside", "Greta Shoal", "Basil Ferryman", "Tessa Longshore",
	"Amos Gangway", "Lotte Seagrass", "Jonah Breakwater", "Vera Coldstream", "Emil Lighthouse",
}

var seedStreets = []string{"Mooring Lane", "Tidal Row", "Chandlers Yard", "Lockkeeper Street"}

// seedIfEmpty adds the starting stock when the database has no species. It writes only
// schema version 1 columns.
func seedIfEmpty(ctx context.Context, tx pgx.Tx, now time.Time) error {
	var count int
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM species`).Scan(&count); err != nil {
		return fmt.Errorf("count species: %w", err)
	}
	if count > 0 {
		return nil
	}
	for _, sp := range seedSpecies {
		_, err := tx.Exec(ctx, `INSERT INTO species (name, daily_price_cents) VALUES ($1, $2)`,
			sp.name, sp.dailyPriceCents)
		if err != nil {
			return fmt.Errorf("seed species %s: %w", sp.name, err)
		}
	}
	for _, t := range seedTanks {
		_, err := tx.Exec(ctx, `INSERT INTO tanks (name, location, capacity, water_temp_c)
			VALUES ($1, $2, $3, $4)`, t.name, t.location, t.capacity, t.tempC)
		if err != nil {
			return fmt.Errorf("seed tank %s: %w", t.name, err)
		}
	}
	if err := seedFish(ctx, tx, now); err != nil {
		return err
	}
	return seedCustomerRows(ctx, tx)
}

// seedFish spreads fish over species, tanks and ages. A few fish are older than the
// retirement age of their species, so release 1.1.0 has fish to retire.
func seedFish(ctx context.Context, tx pgx.Tx, now time.Time) error {
	for i, name := range seedFishNames {
		sp := seedSpecies[i%len(seedSpecies)]
		tank := seedTanks[i%len(seedTanks)]
		percentOfLimit := 2 + ((i*37)%100)*108/100
		ageDays := sp.retireAfterDays * percentOfLimit / 100
		status := "available"
		if i%20 == 7 {
			status = "quarantined"
		}
		_, err := tx.Exec(ctx, `INSERT INTO fish (name, species_id, tank_id, hatch_date, status)
			VALUES ($1, (SELECT id FROM species WHERE name = $2),
			        (SELECT id FROM tanks WHERE name = $3), $4, $5)`,
			name, sp.name, tank.name, now.AddDate(0, 0, -ageDays), status)
		if err != nil {
			return fmt.Errorf("seed fish %s: %w", name, err)
		}
	}
	return nil
}

func seedCustomerRows(ctx context.Context, tx pgx.Tx) error {
	for i, name := range seedCustomers {
		email := strings.ToLower(strings.ReplaceAll(name, " ", ".")) + "@example.com"
		address := fmt.Sprintf("%d %s, Saltmere", 3+i*4, seedStreets[i%len(seedStreets)])
		_, err := tx.Exec(ctx, `INSERT INTO customers (name, email, address) VALUES ($1, $2, $3)`,
			name, email, address)
		if err != nil {
			return fmt.Errorf("seed customer %s: %w", name, err)
		}
	}
	return nil
}
