package saveddata

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nachodev-ui/albion-market-api/internal/accounts"
	"github.com/nachodev-ui/albion-market-api/internal/authn"
)

const (
	PresetLimitEntitlement      = "saved_configurations.max"
	CalculationLimitEntitlement = "saved_calculations.max"

	defaultPresetLimit      = 3
	defaultCalculationLimit = 20
	maxListLimit            = 100
)

var (
	ErrLimitReached = errors.New("saved data limit reached")
	ErrNotFound     = errors.New("saved data not found")
	ErrConflict     = errors.New("saved data conflict")
	ErrInvalid      = errors.New("invalid saved data")
)

var kindPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)

type Preset struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Payload   json.RawMessage `json:"payload"`
	IsDefault bool            `json:"isDefault"`
	CreatedAt time.Time       `json:"createdAt"`
	UpdatedAt time.Time       `json:"updatedAt"`
}

type Calculation struct {
	ID        string          `json:"id"`
	Name      *string         `json:"name"`
	Kind      string          `json:"kind"`
	Snapshot  json.RawMessage `json:"snapshot"`
	CreatedAt time.Time       `json:"createdAt"`
}

type PresetInput struct {
	Name      string          `json:"name"`
	Payload   json.RawMessage `json:"payload"`
	IsDefault bool            `json:"isDefault"`
}

type CalculationInput struct {
	Name     *string         `json:"name"`
	Kind     string          `json:"kind"`
	Snapshot json.RawMessage `json:"snapshot"`
}

type Service struct {
	db       *pgxpool.Pool
	accounts *accounts.Service
}

func NewService(db *pgxpool.Pool, accountService *accounts.Service) *Service {
	return &Service{db: db, accounts: accountService}
}

func (s *Service) ListPresets(ctx context.Context, identity authn.Identity) ([]Preset, error) {
	access, err := s.access(ctx, identity)
	if err != nil {
		return nil, err
	}

	const query = `
		select id::text, name, payload, is_default, created_at, updated_at
		from saved_presets
		where user_id = $1::uuid
		order by is_default desc, updated_at desc, created_at desc
	`
	rows, err := s.db.Query(ctx, query, access.User.ID)
	if err != nil {
		return nil, fmt.Errorf("list presets: %w", err)
	}
	defer rows.Close()

	presets := make([]Preset, 0)
	for rows.Next() {
		preset, scanErr := scanPreset(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		presets = append(presets, preset)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return presets, nil
}

func (s *Service) CreatePreset(ctx context.Context, identity authn.Identity, input PresetInput) (Preset, error) {
	if err := validatePresetInput(input); err != nil {
		return Preset{}, err
	}
	access, err := s.access(ctx, identity)
	if err != nil {
		return Preset{}, err
	}
	limit := entitlementLimit(access.Entitlements, PresetLimitEntitlement, defaultPresetLimit)
	if limit <= 0 {
		return Preset{}, ErrLimitReached
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Preset{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var count int
	if err := tx.QueryRow(ctx, `select count(*) from saved_presets where user_id = $1::uuid`, access.User.ID).Scan(&count); err != nil {
		return Preset{}, fmt.Errorf("count presets: %w", err)
	}
	if count >= limit {
		return Preset{}, ErrLimitReached
	}
	if input.IsDefault {
		if _, err := tx.Exec(ctx, `update saved_presets set is_default = false, updated_at = now() where user_id = $1::uuid and is_default`, access.User.ID); err != nil {
			return Preset{}, fmt.Errorf("clear default preset: %w", err)
		}
	}

	const insert = `
		insert into saved_presets (user_id, name, payload, is_default)
		values ($1::uuid, $2, $3::jsonb, $4)
		returning id::text, name, payload, is_default, created_at, updated_at
	`
	preset, err := scanPreset(tx.QueryRow(ctx, insert, access.User.ID, strings.TrimSpace(input.Name), []byte(input.Payload), input.IsDefault))
	if err != nil {
		return Preset{}, normalizeDatabaseError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Preset{}, err
	}
	return preset, nil
}

func (s *Service) UpdatePreset(ctx context.Context, identity authn.Identity, presetID string, input PresetInput) (Preset, error) {
	if strings.TrimSpace(presetID) == "" {
		return Preset{}, ErrInvalid
	}
	if err := validatePresetInput(input); err != nil {
		return Preset{}, err
	}
	access, err := s.access(ctx, identity)
	if err != nil {
		return Preset{}, err
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Preset{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if input.IsDefault {
		if _, err := tx.Exec(ctx, `update saved_presets set is_default = false, updated_at = now() where user_id = $1::uuid and id::text <> $2 and is_default`, access.User.ID, presetID); err != nil {
			return Preset{}, fmt.Errorf("clear default preset: %w", err)
		}
	}

	const update = `
		update saved_presets
		set name = $3, payload = $4::jsonb, is_default = $5, updated_at = now()
		where user_id = $1::uuid and id::text = $2
		returning id::text, name, payload, is_default, created_at, updated_at
	`
	preset, err := scanPreset(tx.QueryRow(ctx, update, access.User.ID, presetID, strings.TrimSpace(input.Name), []byte(input.Payload), input.IsDefault))
	if err != nil {
		return Preset{}, normalizeDatabaseError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Preset{}, err
	}
	return preset, nil
}

func (s *Service) DeletePreset(ctx context.Context, identity authn.Identity, presetID string) error {
	access, err := s.access(ctx, identity)
	if err != nil {
		return err
	}
	command, err := s.db.Exec(ctx, `delete from saved_presets where user_id = $1::uuid and id::text = $2`, access.User.ID, strings.TrimSpace(presetID))
	if err != nil {
		return fmt.Errorf("delete preset: %w", err)
	}
	if command.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Service) ListCalculations(ctx context.Context, identity authn.Identity, requestedLimit int) ([]Calculation, error) {
	access, err := s.access(ctx, identity)
	if err != nil {
		return nil, err
	}
	entitlementMax := entitlementLimit(access.Entitlements, CalculationLimitEntitlement, defaultCalculationLimit)
	limit := requestedLimit
	if limit <= 0 {
		limit = minInt(entitlementMax, 20)
	}
	limit = minInt(limit, entitlementMax)
	limit = minInt(limit, maxListLimit)
	if limit <= 0 {
		return []Calculation{}, nil
	}

	const query = `
		select id::text, name, kind, snapshot, created_at
		from saved_calculations
		where user_id = $1::uuid
		order by created_at desc
		limit $2
	`
	rows, err := s.db.Query(ctx, query, access.User.ID, limit)
	if err != nil {
		return nil, fmt.Errorf("list calculations: %w", err)
	}
	defer rows.Close()

	calculations := make([]Calculation, 0)
	for rows.Next() {
		calculation, scanErr := scanCalculation(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		calculations = append(calculations, calculation)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return calculations, nil
}

func (s *Service) CreateCalculation(ctx context.Context, identity authn.Identity, input CalculationInput) (Calculation, error) {
	if err := validateCalculationInput(input); err != nil {
		return Calculation{}, err
	}
	access, err := s.access(ctx, identity)
	if err != nil {
		return Calculation{}, err
	}
	limit := entitlementLimit(access.Entitlements, CalculationLimitEntitlement, defaultCalculationLimit)
	if limit <= 0 {
		return Calculation{}, ErrLimitReached
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Calculation{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var count int
	if err := tx.QueryRow(ctx, `select count(*) from saved_calculations where user_id = $1::uuid`, access.User.ID).Scan(&count); err != nil {
		return Calculation{}, fmt.Errorf("count calculations: %w", err)
	}
	if count >= limit {
		return Calculation{}, ErrLimitReached
	}

	kind := strings.TrimSpace(input.Kind)
	if kind == "" {
		kind = "craft"
	}
	var name *string
	if input.Name != nil {
		normalized := strings.TrimSpace(*input.Name)
		if normalized != "" {
			name = &normalized
		}
	}
	const insert = `
		insert into saved_calculations (user_id, name, kind, snapshot)
		values ($1::uuid, $2, $3, $4::jsonb)
		returning id::text, name, kind, snapshot, created_at
	`
	calculation, err := scanCalculation(tx.QueryRow(ctx, insert, access.User.ID, name, kind, []byte(input.Snapshot)))
	if err != nil {
		return Calculation{}, normalizeDatabaseError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Calculation{}, err
	}
	return calculation, nil
}

func (s *Service) DeleteCalculation(ctx context.Context, identity authn.Identity, calculationID string) error {
	access, err := s.access(ctx, identity)
	if err != nil {
		return err
	}
	command, err := s.db.Exec(ctx, `delete from saved_calculations where user_id = $1::uuid and id::text = $2`, access.User.ID, strings.TrimSpace(calculationID))
	if err != nil {
		return fmt.Errorf("delete calculation: %w", err)
	}
	if command.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Service) access(ctx context.Context, identity authn.Identity) (accounts.Access, error) {
	if s == nil || s.db == nil || s.accounts == nil {
		return accounts.Access{}, errors.New("saved data service is not configured")
	}
	return s.accounts.Current(ctx, identity)
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanPreset(row rowScanner) (Preset, error) {
	var preset Preset
	var payload []byte
	if err := row.Scan(&preset.ID, &preset.Name, &payload, &preset.IsDefault, &preset.CreatedAt, &preset.UpdatedAt); err != nil {
		return Preset{}, err
	}
	preset.Payload = append(json.RawMessage(nil), payload...)
	return preset, nil
}

func scanCalculation(row rowScanner) (Calculation, error) {
	var calculation Calculation
	var snapshot []byte
	if err := row.Scan(&calculation.ID, &calculation.Name, &calculation.Kind, &snapshot, &calculation.CreatedAt); err != nil {
		return Calculation{}, err
	}
	calculation.Snapshot = append(json.RawMessage(nil), snapshot...)
	return calculation, nil
}

func validatePresetInput(input PresetInput) error {
	name := strings.TrimSpace(input.Name)
	if name == "" || len([]rune(name)) > 80 || !isJSONObject(input.Payload) {
		return ErrInvalid
	}
	return nil
}

func validateCalculationInput(input CalculationInput) error {
	if input.Name != nil && len([]rune(strings.TrimSpace(*input.Name))) > 120 {
		return ErrInvalid
	}
	kind := strings.TrimSpace(input.Kind)
	if kind != "" && !kindPattern.MatchString(kind) {
		return ErrInvalid
	}
	if !isJSONObject(input.Snapshot) {
		return ErrInvalid
	}
	return nil
}

func isJSONObject(value json.RawMessage) bool {
	if len(value) == 0 || !json.Valid(value) {
		return false
	}
	var object map[string]json.RawMessage
	return json.Unmarshal(value, &object) == nil && object != nil
}

func entitlementLimit(entitlements map[string]any, key string, fallback int) int {
	value, ok := entitlements[key]
	if !ok {
		return fallback
	}
	switch typed := value.(type) {
	case float64:
		return int(typed)
	case int:
		return typed
	case json.Number:
		number, err := typed.Int64()
		if err == nil {
			return int(number)
		}
	}
	return fallback
}

func normalizeDatabaseError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) && postgresError.Code == "23505" {
		return ErrConflict
	}
	return err
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}

type Handler struct {
	service      *Service
	maxBodyBytes int64
}

func NewHandler(service *Service, maxBodyBytes int64) *Handler {
	if maxBodyBytes <= 0 {
		maxBodyBytes = 256 << 10
	}
	return &Handler{service: service, maxBodyBytes: maxBodyBytes}
}

func (h *Handler) Presets(w http.ResponseWriter, r *http.Request) {
	identity, ok := authn.IdentityFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	switch r.Method {
	case http.MethodGet:
		presets, err := h.service.ListPresets(r.Context(), identity)
		if err != nil {
			handleError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"presets": presets})
	case http.MethodPost:
		var input PresetInput
		if err := h.decode(r, &input); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request")
			return
		}
		preset, err := h.service.CreatePreset(r.Context(), identity, input)
		if err != nil {
			handleError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, preset)
	default:
		w.Header().Set("Allow", "GET, POST")
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *Handler) Preset(w http.ResponseWriter, r *http.Request) {
	identity, ok := authn.IdentityFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	presetID := r.PathValue("presetId")
	switch r.Method {
	case http.MethodPut:
		var input PresetInput
		if err := h.decode(r, &input); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request")
			return
		}
		preset, err := h.service.UpdatePreset(r.Context(), identity, presetID, input)
		if err != nil {
			handleError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, preset)
	case http.MethodDelete:
		if err := h.service.DeletePreset(r.Context(), identity, presetID); err != nil {
			handleError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		w.Header().Set("Allow", "PUT, DELETE")
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *Handler) Calculations(w http.ResponseWriter, r *http.Request) {
	identity, ok := authn.IdentityFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	switch r.Method {
	case http.MethodGet:
		requestedLimit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		calculations, err := h.service.ListCalculations(r.Context(), identity, requestedLimit)
		if err != nil {
			handleError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"calculations": calculations})
	case http.MethodPost:
		var input CalculationInput
		if err := h.decode(r, &input); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request")
			return
		}
		calculation, err := h.service.CreateCalculation(r.Context(), identity, input)
		if err != nil {
			handleError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, calculation)
	default:
		w.Header().Set("Allow", "GET, POST")
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *Handler) Calculation(w http.ResponseWriter, r *http.Request) {
	identity, ok := authn.IdentityFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if r.Method != http.MethodDelete {
		w.Header().Set("Allow", http.MethodDelete)
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if err := h.service.DeleteCalculation(r.Context(), identity, r.PathValue("calculationId")); err != nil {
		handleError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) decode(r *http.Request, destination any) error {
	if h == nil || h.service == nil {
		return errors.New("handler unavailable")
	}
	reader := io.LimitReader(r.Body, h.maxBodyBytes+1)
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("unexpected trailing body")
	}
	return nil
}

func handleError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrInvalid):
		writeError(w, http.StatusBadRequest, "invalid saved data")
	case errors.Is(err, ErrLimitReached):
		writeError(w, http.StatusConflict, "saved data limit reached")
	case errors.Is(err, ErrConflict):
		writeError(w, http.StatusConflict, "a saved item with that name already exists")
	case errors.Is(err, ErrNotFound):
		writeError(w, http.StatusNotFound, "saved data not found")
	default:
		writeError(w, http.StatusInternalServerError, "internal server error")
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
