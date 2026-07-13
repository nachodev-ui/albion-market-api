package authn

import (
    "context"
    "crypto"
    "crypto/rsa"
    "crypto/sha256"
    "encoding/base64"
    "encoding/json"
    "errors"
    "fmt"
    "math/big"
    "net/http"
    "net/url"
    "strings"
    "sync"
    "time"
)

var ErrInvalidToken = errors.New("invalid access token")

type Identity struct {
    Subject string
    Email string
    DisplayName string
}

type Config struct {
    Enabled bool
    Issuer string
    Audience string
    CacheTTL time.Duration
    HTTPTimeout time.Duration
    ClockSkew time.Duration
}

type Authenticator struct {
    enabled bool
    validator *Validator
}

func New(cfg Config) (*Authenticator, error) {
    if !cfg.Enabled { return &Authenticator{}, nil }
    issuer, err := url.Parse(strings.TrimRight(strings.TrimSpace(cfg.Issuer), "/") + "/")
    if err != nil || issuer.Scheme == "" || issuer.Host == "" || strings.TrimSpace(cfg.Audience) == "" {
        return nil, fmt.Errorf("configure authentication: issuer and audience are required")
    }
    provider := &JWKSProvider{url: issuer.ResolveReference(&url.URL{Path: ".well-known/jwks.json"}), client: &http.Client{Timeout: cfg.HTTPTimeout}, ttl: cfg.CacheTTL}
    return &Authenticator{enabled: true, validator: &Validator{issuer: issuer.String(), audience: cfg.Audience, keys: provider, clockSkew: cfg.ClockSkew, now: time.Now}}, nil
}

func (a *Authenticator) Require(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Cache-Control", "no-store")
        if a == nil || !a.enabled || a.validator == nil {
            writeError(w, http.StatusServiceUnavailable, "authentication unavailable")
            return
        }
        header := strings.TrimSpace(r.Header.Get("Authorization"))
        if !strings.HasPrefix(header, "Bearer ") {
            writeError(w, http.StatusUnauthorized, "unauthorized")
            return
        }
        identity, err := a.validator.Validate(r.Context(), strings.TrimSpace(strings.TrimPrefix(header, "Bearer ")))
        if err != nil {
            writeError(w, http.StatusUnauthorized, "unauthorized")
            return
        }
        next.ServeHTTP(w, r.WithContext(WithIdentity(r.Context(), identity)))
    })
}

type contextKey struct{}
func WithIdentity(ctx context.Context, identity Identity) context.Context { return context.WithValue(ctx, contextKey{}, identity) }
func IdentityFromContext(ctx context.Context) (Identity, bool) { identity, ok := ctx.Value(contextKey{}).(Identity); return identity, ok }

func writeError(w http.ResponseWriter, status int, message string) {
    w.Header().Set("Content-Type", "application/json; charset=utf-8")
    w.WriteHeader(status)
    _ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}

type keyProvider interface { Key(context.Context, string) (*rsa.PublicKey, error) }
type Validator struct { issuer, audience string; keys keyProvider; clockSkew time.Duration; now func() time.Time }
type header struct { Alg string `json:"alg"`; Kid string `json:"kid"` }
type claims struct { Iss string `json:"iss"`; Sub string `json:"sub"`; Aud audience `json:"aud"`; Exp int64 `json:"exp"`; Nbf int64 `json:"nbf,omitempty"`; Iat int64 `json:"iat,omitempty"`; Email string `json:"email,omitempty"`; Name string `json:"name,omitempty"`; Nickname string `json:"nickname,omitempty"` }
type audience []string
func (a *audience) UnmarshalJSON(data []byte) error { var one string; if json.Unmarshal(data, &one) == nil { *a=[]string{one}; return nil }; var many []string; if err:=json.Unmarshal(data,&many); err!=nil{return err}; *a=many; return nil }
func (a audience) contains(value string) bool { for _, candidate := range a { if candidate == value { return true } }; return false }

func (v *Validator) Validate(ctx context.Context, token string) (Identity, error) {
    parts := strings.Split(strings.TrimSpace(token), ".")
    if len(parts) != 3 { return Identity{}, ErrInvalidToken }
    decode := func(value string, target any) error { raw, err := base64.RawURLEncoding.DecodeString(value); if err != nil { return err }; return json.Unmarshal(raw, target) }
    var h header; var c claims
    if decode(parts[0], &h) != nil || decode(parts[1], &c) != nil || h.Alg != "RS256" || h.Kid == "" { return Identity{}, ErrInvalidToken }
    key, err := v.keys.Key(ctx, h.Kid); if err != nil { return Identity{}, ErrInvalidToken }
    signature, err := base64.RawURLEncoding.DecodeString(parts[2]); if err != nil { return Identity{}, ErrInvalidToken }
    digest := sha256.Sum256([]byte(parts[0]+"."+parts[1]))
    if rsa.VerifyPKCS1v15(key, crypto.SHA256, digest[:], signature) != nil { return Identity{}, ErrInvalidToken }
    now := v.now().Unix(); skew := int64(v.clockSkew.Seconds())
    if c.Iss != v.issuer || !c.Aud.contains(v.audience) || strings.TrimSpace(c.Sub)=="" || c.Exp==0 || now > c.Exp+skew || (c.Nbf!=0 && now+skew<c.Nbf) || (c.Iat!=0 && c.Iat>now+skew) { return Identity{}, ErrInvalidToken }
    name := strings.TrimSpace(c.Name); if name=="" { name=strings.TrimSpace(c.Nickname) }
    return Identity{Subject: strings.TrimSpace(c.Sub), Email: strings.TrimSpace(c.Email), DisplayName: name}, nil
}

type JWKSProvider struct { url *url.URL; client *http.Client; ttl time.Duration; mu sync.RWMutex; expires time.Time; keys map[string]*rsa.PublicKey }
type jwksDocument struct { Keys []jwk `json:"keys"` }
type jwk struct { Kty string `json:"kty"`; Use string `json:"use"`; Alg string `json:"alg"`; Kid string `json:"kid"`; N string `json:"n"`; E string `json:"e"` }
func (p *JWKSProvider) Key(ctx context.Context, kid string) (*rsa.PublicKey, error) {
    p.mu.RLock(); key := p.keys[kid]; fresh := time.Now().Before(p.expires); p.mu.RUnlock()
    if key != nil && fresh { return key, nil }
    if err := p.refresh(ctx); err != nil { return nil, err }
    p.mu.RLock(); defer p.mu.RUnlock(); key=p.keys[kid]; if key==nil{return nil,ErrInvalidToken}; return key,nil
}
func (p *JWKSProvider) refresh(ctx context.Context) error {
    req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.url.String(), nil); if err != nil{return err}
    resp, err := p.client.Do(req); if err != nil{return err}; defer resp.Body.Close()
    if resp.StatusCode != http.StatusOK { return fmt.Errorf("jwks status %d", resp.StatusCode) }
    var doc jwksDocument; if err:=json.NewDecoder(http.MaxBytesReader(nil,resp.Body,1<<20)).Decode(&doc); err!=nil{return err}
    keys:=make(map[string]*rsa.PublicKey)
    for _, item:=range doc.Keys { if item.Kty!="RSA" || item.Kid=="" || item.N=="" || item.E=="" {continue}; nBytes,err:=base64.RawURLEncoding.DecodeString(item.N); if err!=nil{continue}; eBytes,err:=base64.RawURLEncoding.DecodeString(item.E); if err!=nil{continue}; e:=0; for _,b:=range eBytes{e=e<<8+int(b)}; if e<3{continue}; keys[item.Kid]=&rsa.PublicKey{N:new(big.Int).SetBytes(nBytes),E:e} }
    if len(keys)==0{return errors.New("jwks contains no usable keys")}
    ttl:=p.ttl; if ttl<=0{ttl=15*time.Minute}; p.mu.Lock(); p.keys=keys; p.expires=time.Now().Add(ttl); p.mu.Unlock(); return nil
}
