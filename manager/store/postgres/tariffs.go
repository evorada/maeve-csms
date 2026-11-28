// SPDX-License-Identifier: Apache-2.0

package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/thoughtworks/maeve-csms/manager/money"
	"github.com/thoughtworks/maeve-csms/manager/store"
)

// CreateTariff writes the tariff and its first version in a transaction of its own, or
// in a savepoint inside the transaction of the caller.
func (s *Store) CreateTariff(ctx context.Context, t *store.Tariff, v *store.TariffVersion) error {
	id, err := newAssetId(t.Id)
	if err != nil {
		return fmt.Errorf("create tariff: invalid id %q: %w", t.Id, err)
	}
	orgId, err := pgUUID(t.OrgId)
	if err != nil {
		return fmt.Errorf("create tariff: invalid org id %q: %w", t.OrgId, err)
	}
	ocpiId := t.OcpiId
	if ocpiId == "" {
		ocpiId = uuidString(id)
	}
	params, err := tariffVersionParams(id, 1, v)
	if err != nil {
		return fmt.Errorf("create tariff: %w", err)
	}

	tx, err := s.begin(ctx)
	if err != nil {
		return fmt.Errorf("create tariff: begin transaction: %w", err)
	}
	defer rollback(ctx, tx)
	q := New(tx)

	row, err := q.CreateTariff(ctx, CreateTariffParams{
		ID:       id,
		OrgID:    orgId,
		PartyRef: t.PartyRef,
		OcpiID:   ocpiId,
		Name:     t.Name,
	})
	if err != nil {
		return assetWriteError("create tariff", err)
	}
	versionRow, err := q.CreateTariffVersion(ctx, params)
	if err != nil {
		return fmt.Errorf("create tariff: version 1: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("create tariff: commit: %w", err)
	}

	stored, err := tariffFromRow(row, versionRow)
	if err != nil {
		return err
	}
	// the caller gets a version of its own, not the one that the tariff points to
	version, err := tariffVersionFromRow(versionRow)
	if err != nil {
		return err
	}
	*t = *stored
	*v = *version
	return nil
}

// AddTariffVersion moves the tariff to its new version first: that locks the row of the
// tariff, so that of two callers with the same expected version the second finds the
// tariff moved on. It runs in a transaction of its own, or in a savepoint inside the
// transaction of the caller.
func (s *Store) AddTariffVersion(ctx context.Context, tariffId string, expectedCurrent int, v *store.TariffVersion) error {
	what := "add a version to tariff " + tariffId
	id, ok := assetId(tariffId)
	if !ok {
		return fmt.Errorf("%s: %w", what, store.ErrNotFound)
	}

	tx, err := s.begin(ctx)
	if err != nil {
		return fmt.Errorf("%s: begin transaction: %w", what, err)
	}
	defer rollback(ctx, tx)
	q := New(tx)

	// a version that is no int32, or the last one that is, is not the current version of
	// a tariff that can take another
	expected, fits := assetNumber(expectedCurrent)
	moved := int64(0)
	if fits && expectedCurrent > 0 && expectedCurrent < maxTariffVersion {
		moved, err = q.BumpTariffCurrentVersion(ctx, BumpTariffCurrentVersionParams{
			NewVersion:      expected + 1,
			ID:              id,
			ExpectedVersion: expected,
		})
		if err != nil {
			return fmt.Errorf("%s: %w", what, err)
		}
	}
	if moved == 0 {
		row, err := q.GetTariff(ctx, id)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return fmt.Errorf("%s: %w", what, store.ErrNotFound)
		case err != nil:
			return fmt.Errorf("%s: %w", what, err)
		case row.Tariff.DeletedAt.Valid:
			return fmt.Errorf("%s: the tariff is deleted: %w", what, store.ErrNotFound)
		default:
			return fmt.Errorf("%s: the current version is %d, not %d: %w", what, row.Tariff.CurrentVersion, expectedCurrent, store.ErrVersionConflict)
		}
	}

	params, err := tariffVersionParams(id, expected+1, v)
	if err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}
	versionRow, err := q.CreateTariffVersion(ctx, params)
	if err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("%s: commit: %w", what, err)
	}

	version, err := tariffVersionFromRow(versionRow)
	if err != nil {
		return err
	}
	*v = *version
	return nil
}

// maxTariffVersion is the largest version number that the INT column holds.
const maxTariffVersion = 1<<31 - 1

func (s *Store) RenameTariff(ctx context.Context, tariffId, name string) error {
	id, ok := assetId(tariffId)
	if !ok {
		return fmt.Errorf("rename tariff %s: %w", tariffId, store.ErrNotFound)
	}
	renamed, err := s.writeQueries(ctx).RenameTariff(ctx, RenameTariffParams{Name: name, ID: id})
	if err != nil {
		return fmt.Errorf("rename tariff %s: %w", tariffId, err)
	}
	if renamed == 0 {
		return fmt.Errorf("rename tariff %s: %w", tariffId, store.ErrNotFound)
	}
	return nil
}

func (s *Store) LookupTariff(ctx context.Context, tariffId string) (*store.Tariff, error) {
	id, ok := assetId(tariffId)
	if !ok {
		return nil, nil
	}
	row, err := s.readQueries(ctx).GetTariff(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("lookup tariff %s: %w", tariffId, err)
	}
	return tariffFromRow(row.Tariff, row.TariffVersion)
}

func (s *Store) LookupTariffByOcpiId(ctx context.Context, partyRef, ocpiId string) (*store.Tariff, error) {
	row, err := s.readQueries(ctx).GetTariffByOcpiId(ctx, GetTariffByOcpiIdParams{PartyRef: partyRef, OcpiID: ocpiId})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("lookup tariff %s/%s: %w", partyRef, ocpiId, err)
	}
	return tariffFromRow(row.Tariff, row.TariffVersion)
}

func (s *Store) LookupTariffVersion(ctx context.Context, tariffId string, version int) (*store.TariffVersion, error) {
	id, ok := assetId(tariffId)
	if !ok {
		return nil, nil
	}
	number, ok := assetNumber(version)
	if !ok {
		return nil, nil
	}
	row, err := s.readQueries(ctx).GetTariffVersion(ctx, GetTariffVersionParams{TariffID: id, Version: number})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("lookup version %d of tariff %s: %w", version, tariffId, err)
	}
	return tariffVersionFromRow(row)
}

func (s *Store) ListTariffs(ctx context.Context, filter store.TariffFilter) ([]*store.Tariff, string, error) {
	afterId, err := store.DecodeCursor(filter.Page.Cursor)
	if err != nil {
		return nil, "", fmt.Errorf("list tariffs: %w", err)
	}
	pgAfterId, err := pgUUID(afterId)
	if err != nil {
		return nil, "", fmt.Errorf("list tariffs: %w", err)
	}
	orgIds := make([]pgtype.UUID, len(filter.OrgIds))
	for i, orgId := range filter.OrgIds {
		if orgIds[i], err = pgUUID(orgId); err != nil {
			return nil, "", fmt.Errorf("list tariffs: invalid org id %q: %w", orgId, err)
		}
	}
	if len(orgIds) == 0 {
		return []*store.Tariff{}, "", nil
	}
	limit := filter.Page.EffectiveLimit()
	// one more row than the page tells whether there is a next page
	rowLimit, err := safeIntToInt32(limit + 1)
	if err != nil {
		return nil, "", fmt.Errorf("list tariffs: %w", err)
	}
	query := pgtype.Text{}
	if filter.Query != nil {
		query = pgtype.Text{String: likeEscaper.Replace(*filter.Query), Valid: true}
	}
	tariffType := pgtype.Text{}
	if filter.Type != nil {
		tariffType = pgtype.Text{String: string(*filter.Type), Valid: true}
	}

	rows, err := s.readQueries(ctx).ListTariffs(ctx, ListTariffsParams{
		OrgIds:         orgIds,
		PartyRef:       toPgText(filter.PartyRef),
		Type:           tariffType,
		Query:          query,
		IncludeDeleted: filter.IncludeDeleted,
		AfterID:        pgAfterId,
		RowLimit:       rowLimit,
	})
	if err != nil {
		return nil, "", fmt.Errorf("list tariffs: %w", err)
	}

	nextCursor := ""
	if len(rows) > limit {
		rows = rows[:limit]
		nextCursor = store.EncodeCursor(uuidString(rows[limit-1].Tariff.ID))
	}
	tariffs := make([]*store.Tariff, len(rows))
	for i, row := range rows {
		if tariffs[i], err = tariffFromRow(row.Tariff, row.TariffVersion); err != nil {
			return nil, "", err
		}
	}
	return tariffs, nextCursor, nil
}

func (s *Store) ListTariffVersions(ctx context.Context, tariffId string) ([]*store.TariffVersion, error) {
	id, ok := assetId(tariffId)
	if !ok {
		return []*store.TariffVersion{}, nil
	}
	rows, err := s.readQueries(ctx).ListTariffVersions(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("list the versions of tariff %s: %w", tariffId, err)
	}
	versions := make([]*store.TariffVersion, len(rows))
	for i, row := range rows {
		if versions[i], err = tariffVersionFromRow(row); err != nil {
			return nil, err
		}
	}
	return versions, nil
}

// DeleteTariff counts the connectors that refer to the tariff and marks the tariff as
// deleted, in a transaction of its own or in a savepoint inside the transaction of the
// caller.
func (s *Store) DeleteTariff(ctx context.Context, tariffId string) error {
	id, ok := assetId(tariffId)
	if !ok {
		return nil
	}
	tx, err := s.begin(ctx)
	if err != nil {
		return fmt.Errorf("delete tariff %s: begin transaction: %w", tariffId, err)
	}
	defer rollback(ctx, tx)
	q := New(tx)

	row, err := q.GetTariff(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("delete tariff %s: %w", tariffId, err)
	}
	if row.Tariff.DeletedAt.Valid {
		return nil
	}
	references, err := q.CountTariffReferences(ctx, id)
	if err != nil {
		return fmt.Errorf("delete tariff %s: count references: %w", tariffId, err)
	}
	if references > 0 {
		return fmt.Errorf("delete tariff %s: %d connectors: %w", tariffId, references, store.ErrInUse)
	}
	if err := q.SoftDeleteTariff(ctx, id); err != nil {
		return fmt.Errorf("delete tariff %s: %w", tariffId, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("delete tariff %s: commit: %w", tariffId, err)
	}
	return nil
}

// tariffVersionParams returns the row to insert for version number `version` of the
// tariff.
func tariffVersionParams(tariffId pgtype.UUID, version int32, v *store.TariffVersion) (CreateTariffVersionParams, error) {
	params := CreateTariffVersionParams{
		TariffID:      tariffId,
		Version:       version,
		Currency:      v.Currency,
		TariffAltUrl:  toPgText(v.AltUrl),
		StartDateTime: toNullTimestamptz(v.StartDateTime),
		EndDateTime:   toNullTimestamptz(v.EndDateTime),
		CreatedBy:     v.CreatedBy,
	}
	if v.Type != nil {
		params.Type = pgtype.Text{String: string(*v.Type), Valid: true}
	}
	if v.MinPrice != nil {
		params.MinPriceExclVat = numericFromAmount(v.MinPrice.ExclVat)
		params.MinPriceInclVat = numericFromAmount(v.MinPrice.InclVat)
	}
	if v.MaxPrice != nil {
		params.MaxPriceExclVat = numericFromAmount(v.MaxPrice.ExclVat)
		params.MaxPriceInclVat = numericFromAmount(v.MaxPrice.InclVat)
	}

	// a nil list is stored as an empty list: the columns are NOT NULL
	altText := v.AltText
	if altText == nil {
		altText = []store.DisplayText{}
	}
	elements := v.Elements
	if elements == nil {
		elements = []store.TariffElement{}
	}
	var err error
	if params.TariffAltText, err = json.Marshal(altText); err != nil {
		return params, fmt.Errorf("alt text: %w", err)
	}
	if params.Elements, err = json.Marshal(elements); err != nil {
		return params, fmt.Errorf("elements: %w", err)
	}
	if len(v.EnergyMix) > 0 {
		params.EnergyMix = v.EnergyMix
	}
	if v.IdleDetection != nil {
		if params.IdleDetection, err = json.Marshal(v.IdleDetection); err != nil {
			return params, fmt.Errorf("idle detection: %w", err)
		}
	}
	return params, nil
}

func tariffFromRow(row Tariff, current TariffVersion) (*store.Tariff, error) {
	version, err := tariffVersionFromRow(current)
	if err != nil {
		return nil, err
	}
	return &store.Tariff{
		Id:             uuidString(row.ID),
		OrgId:          uuidString(row.OrgID),
		PartyRef:       row.PartyRef,
		OcpiId:         row.OcpiID,
		Name:           row.Name,
		CurrentVersion: int(row.CurrentVersion),
		Current:        version,
		DeletedAt:      fromNullTimestamptz(row.DeletedAt),
		CreatedAt:      fromPgTimestamptz(row.CreatedAt),
		UpdatedAt:      fromPgTimestamptz(row.UpdatedAt),
	}, nil
}

func tariffVersionFromRow(row TariffVersion) (*store.TariffVersion, error) {
	what := fmt.Sprintf("version %d of tariff %s", row.Version, uuidString(row.TariffID))
	v := &store.TariffVersion{
		TariffId:      uuidString(row.TariffID),
		Version:       int(row.Version),
		Currency:      row.Currency,
		AltText:       []store.DisplayText{},
		AltUrl:        fromPgText(row.TariffAltUrl),
		StartDateTime: fromNullTimestamptz(row.StartDateTime),
		EndDateTime:   fromNullTimestamptz(row.EndDateTime),
		Elements:      []store.TariffElement{},
		CreatedAt:     fromPgTimestamptz(row.CreatedAt),
		CreatedBy:     row.CreatedBy,
	}
	if row.Type.Valid {
		tariffType := store.TariffType(row.Type.String)
		v.Type = &tariffType
	}
	var err error
	if v.MinPrice, err = priceFromNumerics(row.MinPriceExclVat, row.MinPriceInclVat); err != nil {
		return nil, fmt.Errorf("%s: min price: %w", what, err)
	}
	if v.MaxPrice, err = priceFromNumerics(row.MaxPriceExclVat, row.MaxPriceInclVat); err != nil {
		return nil, fmt.Errorf("%s: max price: %w", what, err)
	}
	if err := json.Unmarshal(row.TariffAltText, &v.AltText); err != nil {
		return nil, fmt.Errorf("%s: alt text: %w", what, err)
	}
	if err := json.Unmarshal(row.Elements, &v.Elements); err != nil {
		return nil, fmt.Errorf("%s: elements: %w", what, err)
	}
	// a JSON null in a column is an empty list too
	if v.AltText == nil {
		v.AltText = []store.DisplayText{}
	}
	if v.Elements == nil {
		v.Elements = []store.TariffElement{}
	}
	if len(row.EnergyMix) > 0 {
		v.EnergyMix = json.RawMessage(row.EnergyMix)
	}
	if len(row.IdleDetection) > 0 {
		v.IdleDetection = &store.IdleDetection{}
		if err := json.Unmarshal(row.IdleDetection, v.IdleDetection); err != nil {
			return nil, fmt.Errorf("%s: idle detection: %w", what, err)
		}
	}
	return v, nil
}

// priceFromNumerics returns the price of two NUMERIC columns; nil when they are NULL.
func priceFromNumerics(exclVat, inclVat pgtype.Numeric) (*money.Price, error) {
	excl, hasExcl, err := amountFromNumeric(exclVat)
	if err != nil {
		return nil, err
	}
	incl, hasIncl, err := amountFromNumeric(inclVat)
	if err != nil {
		return nil, err
	}
	if !hasExcl || !hasIncl {
		return nil, nil
	}
	return &money.Price{ExclVat: excl, InclVat: incl}, nil
}
