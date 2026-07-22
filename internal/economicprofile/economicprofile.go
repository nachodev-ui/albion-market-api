package economicprofile

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nachodev-ui/albion-market-api/internal/accounts"
	"github.com/nachodev-ui/albion-market-api/internal/authn"
)

var (
	ErrInvalid = errors.New("invalid economic profile")
	ErrNotFound = errors.New("economic profile not found")
)

var branchKeyPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

var validServers = map[string]struct{}{
	"americas": {},
	"europe":   {},
	"asia":     {},
}

var validCities = map[string]struct{}{
	"bridgewatch":   {},
	"caerleon":      {},
	"fort-sterling": {},
	"lymhurst":      {},
	"martlock":      {},
	"thetford":      {},
	"brecilien":     {},
}

type Specialization struct {
	BranchKey           string `json:"branchKey"`
	BranchName          string `json:"branchName"`
	Level               int    `json:"level"`
	FocusCostEfficiency int    `json:"focusCostEfficiency"`
}

type Profile struct {
	Server              string           `json:"server"`
	PremiumActive       bool             `json:"premiumActive"`
	DailyFocusBalance   int              `json:"dailyFocusBalance"`
	HomeCity            string           `json:"homeCity"`
	GuildHasIsland      bool             `json:"guildHasIsland"`
	SalesTaxRate        float64          `json:"salesTaxRate"`
	TransportCost       int64            `json:"transportCost"`
	Specializations     []Specialization `json:"specializations"`
	CreatedAt           time.Time        `json:"createdAt"`
	UpdatedAt           time.Time        `json:"updatedAt"`
}

type Input struct {
	Server              string           `json:"server"`
	PremiumActive       bool             `json:"premiumActive"`
	DailyFocusBalance   int              `json:"dailyFocusBalance"`
	HomeCity            string           `json:"homeCity"`
	GuildHasIsland      bool             `json:"guildHasIsland"`
	SalesTaxRate        float64          `json:"salesTaxRate"`
	TransportCost       int64            `json:"transportCost"`
	Specializations     []Specialization `json:"specializations"`
}

type Service struct {
	db       *pgxpool.Pool
	accounts *accounts.Service
}

func NewService(db *pgxpool.Pool, accountService *accounts.Service) *Service {
	return &Service{db: db, accounts: accountService}
}

func (s *Service) Current(ctx context.Context, identity authn.Identity) (*Profile, error) {
	access, err := s.access(ctx, identity)
	if err != nil {
		return nil, err
	}

	const query = `
		select server, premium_active, daily_focus_balance, home_city,
		       guild_has_island, sales_tax_rate, transport_cost,
		       specializations, created_at, updated_at
		from player_economic_profiles
		where user_id = $1::uuid
	`
	profile, err := scanProfile(s.db.QueryRow(ctx, query, access.User.ID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load economic profile: %w", err)
	}
	return &profile, nil
}

func (s *Service) Save(ctx context.Context, identity authn.Identity, input Input) (Profile, error) {
	normalized, err := normalizeInput(input)
	if err != nil {
		return Profile{}, err
	}
	access, err := s.access(ctx, identity)
	if err != nil {
		return Profile{}, err
	}
	specializations, err := json.Marshal(normalized.Specializations)
	if err != nil {
		return Profile{}, fmt.Errorf("encode specializations: %w", err)
	}

	const query = `
		insert into player_economic_profiles (
			user_id, server, premium_active, daily_focus_balance, home_city,
			guild_has_island, sales_tax_rate, transport_cost, specializations
		)
		values ($1::uuid, $2, $3, $4, $5, $6, $7, $8, $9::jsonb)
		on conflict (user_id) do update set
			server = excluded.server,
			premium_active = excluded.premium_active,
			daily_focus_balance = excluded.daily_focus_balance,
			home_city = excluded.home_city,
			guild_has_island = excluded.guild_has_island,
			sales_tax_rate = excluded.sales_tax_rate,
			transport_cost = excluded.transport_cost,
			specializations = excluded.specializations,
			updated_at = now()
		returning server, premium_active, daily_focus_balance, home_city,
		          guild_has_island, sales_tax_rate, transport_cost,
		          specializations, created_at, updated_at
	`
	profile, err := scanProfile(s.db.QueryRow(
		ctx,
		query,
		access.User.ID,
		normalized.Server,
		normalized.PremiumActive,
		normalized.DailyFocusBalance,
		normalized.HomeCity,
		normalized.GuildHasIsland,
		normalized.SalesTaxRate,
		normalized.TransportCost,
		specializations,
	))
	if err != nil {
		return Profile{}, fmt.Errorf("save economic profile: %w", err)
	}
	return profile, nil
}

func (s *Service) Delete(ctx context.Context, identity authn.Identity) error {
	access, err := s.access(ctx, identity)
	if err != nil {
		return err
	}
	command, err := s.db.Exec(ctx, `delete from player_economic_profiles where user_id = $1::uuid`, access.User.ID)
	if err != nil {
		return fmt.Errorf("delete economic profile: %w", err)
	}
	if command.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Service) access(ctx context.Context, identity authn.Identity) (accounts.Access, error) {
	if s == nil || s.db == nil || s.accounts == nil {
		return accounts.Access{}, errors.New("economic profile service is not configured")
	}
	return s.accounts.Current(ctx, identity)
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanProfile(row rowScanner) (Profile, error) {
	var profile Profile
	var rawSpecializations []byte
	if err := row.Scan(
		&profile.Server,
		&profile.PremiumActive,
		&profile.DailyFocusBalance,
		&profile.HomeCity,
		&profile.GuildHasIsland,
		&profile.SalesTaxRate,
		&profile.TransportCost,
		&rawSpecializations,
		&profile.CreatedAt,
		&profile.UpdatedAt,
	); err != nil {
		return Profile{}, err
	}
	if err := json.Unmarshal(rawSpecializations, &profile.Specializations); err != nil {
		return Profile{}, fmt.Errorf("decode specializations: %w", err)
	}
	if profile.Specializations == nil {
		profile.Specializations = []Specialization{}
	}
	return profile, nil
}

func normalizeInput(input Input) (Input, error) {
	input.Server = strings.ToLower(strings.TrimSpace(input.Server))
	input.HomeCity = strings.ToLower(strings.TrimSpace(input.HomeCity))
	if _, ok := validServers[input.Server]; !ok {
		return Input{}, fmt.Errorf("%w: unsupported server", ErrInvalid)
	}
	if _, ok := validCities[input.HomeCity]; !ok {
		return Input{}, fmt.Errorf("%w: unsupported home city", ErrInvalid)
	}
	if input.DailyFocusBalance < 0 || input.DailyFocusBalance > 100000 {
		return Input{}, fmt.Errorf("%w: daily focus balance is out of range", ErrInvalid)
	}
	if input.SalesTaxRate < 0 || input.SalesTaxRate > 100 {
		return Input{}, fmt.Errorf("%w: sales tax rate is out of range", ErrInvalid)
	}
	if input.TransportCost < 0 || input.TransportCost > 1_000_000_000_000 {
		return Input{}, fmt.Errorf("%w: transport cost is out of range", ErrInvalid)
	}
	if len(input.Specializations) > 50 {
		return Input{}, fmt.Errorf("%w: too many specializations", ErrInvalid)
	}

	seen := make(map[string]struct{}, len(input.Specializations))
	for index := range input.Specializations {
		specialization := &input.Specializations[index]
		specialization.BranchKey = strings.ToLower(strings.TrimSpace(specialization.BranchKey))
		specialization.BranchName = strings.TrimSpace(specialization.BranchName)
		if !branchKeyPattern.MatchString(specialization.BranchKey) {
			return Input{}, fmt.Errorf("%w: invalid specialization branch key", ErrInvalid)
		}
		if len([]rune(specialization.BranchName)) < 1 || len([]rune(specialization.BranchName)) > 80 {
			return Input{}, fmt.Errorf("%w: invalid specialization branch name", ErrInvalid)
		}
		if specialization.Level < 0 || specialization.Level > 100 {
			return Input{}, fmt.Errorf("%w: specialization level is out of range", ErrInvalid)
		}
		if specialization.FocusCostEfficiency < 0 || specialization.FocusCostEfficiency > 100000 {
			return Input{}, fmt.Errorf("%w: focus cost efficiency is out of range", ErrInvalid)
		}
		if _, exists := seen[specialization.BranchKey]; exists {
			return Input{}, fmt.Errorf("%w: duplicate specialization branch", ErrInvalid)
		}
		seen[specialization.BranchKey] = struct{}{}
	}
	if input.Specializations == nil {
		input.Specializations = []Specialization{}
	}
	return input, nil
}

type serviceAPI interface {
	Current(context.Context, authn.Identity) (*Profile, error)
	Save(context.Context, authn.Identity, Input) (Profile, error)
	Delete(context.Context, authn.Identity) error
}

type Handler struct {
	service      serviceAPI
	maxBodyBytes int64
}

func NewHandler(service serviceAPI, maxBodyBytes int64) *Handler {
	if maxBodyBytes <= 0 {
		maxBodyBytes = 64 << 10
	}
	return &Handler{service: service, maxBodyBytes: maxBodyBytes}
}

func (h *Handler) Profile(w http.ResponseWriter, r *http.Request) {
	identity, ok := requestIdentity(w, r)
	if !ok {
		return
	}

	switch r.Method {
	case http.MethodGet:
		profile, err := h.service.Current(r.Context(), identity)
		if err != nil {
			h.writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"profile": profile})
	case http.MethodPut:
		var input Input
		if err := decodeJSON(r, h.maxBodyBytes, &input); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		profile, err := h.service.Save(r.Context(), identity, input)
		if err != nil {
			h.writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"profile": profile})
	case http.MethodDelete:
		if err := h.service.Delete(r.Context(), identity); err != nil {
			h.writeError(w, err)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusNoContent)
	default:
		w.Header().Set("Allow", "GET, PUT, DELETE")
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

func (h *Handler) writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrInvalid):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
	case errors.Is(err, ErrNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "economic profile not found"})
	default:
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
	}
}

func requestIdentity(w http.ResponseWriter, r *http.Request) (authn.Identity, bool) {
	w.Header().Set("Cache-Control", "no-store")
	identity, ok := authn.IdentityFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return authn.Identity{}, false
	}
	return identity, true
}

func decodeJSON(r *http.Request, maxBodyBytes int64, destination any) error {
	defer r.Body.Close()
	decoder := json.NewDecoder(io.LimitReader(r.Body, maxBodyBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return errors.New("invalid JSON request")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return errors.New("request must contain one JSON object")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
