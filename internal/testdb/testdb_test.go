package testdb_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/vumanhcuongit/service-bay-scheduler/internal/testdb"
)

func TestSeedLoaded(t *testing.T) {
	ctx := context.Background()
	tx := testdb.Tx(t)

	counts := map[string]int{
		"dealerships":   2,
		"skills":        5,
		"bays":          3,
		"technicians":   4,
		"service_types": 5,
		"customers":     1,
		"vehicles":      3,
	}
	for table, want := range counts {
		var got int
		if err := tx.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&got); err != nil {
			t.Fatalf("%s: %v", table, err)
		}
		if got != want {
			t.Errorf("%s: got %d rows, want %d", table, got, want)
		}
	}

	var workEnd string
	if err := tx.QueryRow(ctx, "SELECT work_end::text FROM technicians WHERE id = 'T2'").Scan(&workEnd); err != nil {
		t.Fatal(err)
	}
	if workEnd != "13:00:00" {
		t.Errorf("T2 work_end = %s, want 13:00:00", workEnd)
	}

	var skill string
	var qualified int
	if err := tx.QueryRow(ctx, `
		SELECT st.required_skill, (SELECT count(*) FROM technician_skills ts WHERE ts.skill_id = st.required_skill)
		FROM service_types st WHERE st.id = 'TRANSMISSION'`).Scan(&skill, &qualified); err != nil {
		t.Fatal(err)
	}
	if skill != "transmission" || qualified != 0 {
		t.Errorf("TRANSMISSION: skill %q with %d technicians, want transmission with 0", skill, qualified)
	}
}

func TestExclusionConstraintsArePartialOnStatus(t *testing.T) {
	ctx := context.Background()
	tx := testdb.Tx(t)

	insert := `INSERT INTO appointments
		(dealership_id, customer_id, vehicle_id, service_type_id, bay_id, technician_id, start, "end")
		VALUES ('DL1', 'C1', $1, 'OIL', $2, $3, $4, $5)`

	if _, err := tx.Exec(ctx, insert, "V1", "B1", "T1", "2026-10-21T02:00:00Z", "2026-10-21T02:30:00Z"); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name, vehicle, bay, tech, start, end, constraint string
	}{
		{"bay", "V2", "B1", "T3", "2026-10-21T02:15:00Z", "2026-10-21T02:45:00Z", "appointments_bay_excl"},
		{"technician", "V2", "B2", "T1", "2026-10-21T02:15:00Z", "2026-10-21T02:45:00Z", "appointments_technician_excl"},
		{"vehicle", "V1", "B2", "T3", "2026-10-21T02:15:00Z", "2026-10-21T02:45:00Z", "appointments_vehicle_excl"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sp, err := tx.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = sp.Rollback(ctx) }()
			_, err = sp.Exec(ctx, insert, c.vehicle, c.bay, c.tech, c.start, c.end)
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || pgErr.Code != "23P01" || pgErr.ConstraintName != c.constraint {
				t.Fatalf("got %v, want 23P01 on %s", err, c.constraint)
			}
		})
	}

	// status only allows 'confirmed' today, so partial-ness is checked on the definition.
	rows, err := tx.Query(ctx, `
		SELECT conname, pg_get_constraintdef(oid) FROM pg_constraint
		WHERE conrelid = 'appointments'::regclass AND contype = 'x'`)
	if err != nil {
		t.Fatal(err)
	}
	defs := map[string]string{}
	for rows.Next() {
		var name, def string
		if err := rows.Scan(&name, &def); err != nil {
			t.Fatal(err)
		}
		defs[name] = def
	}
	if len(defs) != 3 {
		t.Fatalf("got %d exclusion constraints, want 3: %v", len(defs), defs)
	}
	for name, def := range defs {
		if !strings.Contains(def, "WHERE ((status = 'confirmed'::text))") {
			t.Errorf("%s is not partial on status: %s", name, def)
		}
	}

	// Half-open: a booking starting where the other ends does not overlap.
	if _, err := tx.Exec(ctx, insert, "V2", "B1", "T1", "2026-10-21T02:30:00Z", "2026-10-21T03:00:00Z"); err != nil {
		t.Fatalf("back to back: %v", err)
	}
}

// The overlap reads are served by the gist indexes behind the exclusion constraints. A btree on
// (dealership_id, start) cannot seek on an overlap predicate and was dropped (migration 00003),
// so a second index on appointments is an addition someone has to justify.
func TestAppointmentsIndexes(t *testing.T) {
	ctx := context.Background()
	tx := testdb.Tx(t)
	rows, err := tx.Query(ctx, `SELECT indexname FROM pg_indexes WHERE tablename = 'appointments' ORDER BY indexname`)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		got = append(got, name)
	}
	want := []string{"appointments_bay_excl", "appointments_pkey", "appointments_technician_excl", "appointments_vehicle_excl"}
	if !slices.Equal(got, want) {
		t.Errorf("indexes on appointments = %v, want %v", got, want)
	}
}
