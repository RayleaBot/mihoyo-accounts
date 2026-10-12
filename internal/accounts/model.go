package accounts

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"strings"
)

type Subject struct {
	SourceProtocol string `json:"source_protocol"`
	BotID          string `json:"bot_id"`
	ActorID        string `json:"actor_id"`
}

func (s Subject) Validate() error {
	for _, value := range []string{s.SourceProtocol, s.BotID, s.ActorID} {
		if value == "" || len(value) > 256 {
			return errors.New("account subject is incomplete")
		}
	}
	return nil
}

func (s Subject) Key() string {
	data, _ := json.Marshal(s)
	digest := sha256.Sum256(data)
	return "subject." + hex.EncodeToString(digest[:])
}

func (s Subject) Equal(other Subject) bool {
	return s.SourceProtocol == other.SourceProtocol && s.BotID == other.BotID && s.ActorID == other.ActorID
}

type Tokens struct {
	Realm               string `json:"realm,omitempty"`
	LTokenV2            bool   `json:"ltoken_v2,omitempty"`
	CookieTokenV2       bool   `json:"cookie_token_v2,omitempty"`
	STokenV2            bool   `json:"stoken_v2,omitempty"`
	CloudGenshin        string `json:"cloud_genshin,omitempty"`
	CloudZZZ            string `json:"cloud_zzz,omitempty"`
	AccountID           string `json:"account_id"`
	MID                 string `json:"mid,omitempty"`
	SToken              string `json:"stoken,omitempty"`
	LToken              string `json:"ltoken,omitempty"`
	CookieToken         string `json:"cookie_token,omitempty"`
	DeviceID            string `json:"device_id"`
	DeviceFP            string `json:"device_fp,omitempty"`
	DeviceFPExpiresAtMS int64  `json:"device_fp_expires_at_ms,omitempty"`
	// Device is an Android device the owner imported for device checks, as
	// ZZZ-Plugin's 绑定设备 takes it. DeviceFPImported marks a fingerprint
	// the owner imported with its device ID, kept rather than renewed.
	Device           *Device `json:"device,omitempty"`
	DeviceFPImported bool    `json:"device_fp_imported,omitempty"`
}

// Device is the device information ZZZ-Plugin's 绑定设备 reads, with its
// upstream field names.
type Device struct {
	Name           string `json:"deviceName"`
	Board          string `json:"deviceBoard"`
	Model          string `json:"deviceModel"`
	OAID           string `json:"oaid"`
	AndroidVersion string `json:"androidVersion"`
	Fingerprint    string `json:"deviceFingerprint"`
	Product        string `json:"deviceProduct"`
}

// Brand and Display are parts of the build fingerprint, as brand/product/
// device:version/display/...
func (d Device) Brand() string { return strings.Split(d.Fingerprint, "/")[0] }
func (d Device) Display() string {
	if parts := strings.Split(d.Fingerprint, "/"); len(parts) > 3 {
		return parts[3]
	}
	return ""
}

type Role struct {
	Ref      string `json:"ref"`
	Game     string `json:"game"`
	GameBiz  string `json:"game_biz"`
	UID      string `json:"uid"`
	Region   string `json:"region"`
	Nickname string `json:"nickname"`
	Level    int    `json:"level"`
}

// Record is only serialized by the encrypted repository. Public results use
// Summary, which has no credential fields.
type Record struct {
	CloudStates    map[string]map[string]any `json:"cloud_states,omitempty"`
	LastCheck      *CheckResult              `json:"last_check,omitempty"`
	PublicQuery    *PublicPolicy             `json:"public_query,omitempty"`
	Delegations    []Delegation              `json:"delegations,omitempty"`
	Ref            string                    `json:"ref"`
	Owner          Subject                   `json:"owner"`
	Revision       uint64                    `json:"revision"`
	Tokens         Tokens                    `json:"tokens"`
	Roles          []Role                    `json:"roles"`
	AllowedCallers []string                  `json:"allowed_callers"`
	Status         string                    `json:"status"`
	UpdatedAtMS    int64                     `json:"updated_at_ms"`
	Deleted        bool                      `json:"deleted,omitempty"`
	CatalogPage    int                       `json:"catalog_page"`
}

type Delegation struct {
	Owner        Subject `json:"owner,omitempty"`
	Ref          string  `json:"ref"`
	Caller       string  `json:"caller"`
	TaskID       string  `json:"task_id"`
	RoleRef      string  `json:"role_ref"`
	Operation    string  `json:"operation"`
	ExpiresAtMS  int64   `json:"expires_at_ms"`
	LastUsedAtMS int64   `json:"last_used_at_ms"`
}

type Summary struct {
	Realm           string        `json:"realm"`
	Shared          bool          `json:"shared"`
	CloudConfigured []string      `json:"cloud_configured"`
	LastCheck       *CheckResult  `json:"last_check,omitempty"`
	PublicQuery     *PublicPolicy `json:"public_query,omitempty"`
	Ref             string        `json:"ref"`
	Revision        uint64        `json:"revision"`
	Owner           Subject       `json:"owner"`
	AccountLabel    string        `json:"account_label"`
	Roles           []Role        `json:"roles"`
	AllowedCallers  []string      `json:"allowed_callers"`
	Status          string        `json:"status"`
	CanRefresh      bool          `json:"can_refresh"`
	UpdatedAtMS     int64         `json:"updated_at_ms"`
}

func (r Record) Summary() Summary {
	roles := append([]Role{}, r.Roles...)
	callers := append([]string{}, r.AllowedCallers...)
	var public *PublicPolicy
	if r.PublicQuery != nil {
		copy := *r.PublicQuery
		copy.Games = append([]string{}, copy.Games...)
		public = &copy
	}
	var check *CheckResult
	if r.LastCheck != nil {
		value := *r.LastCheck
		check = &value
	}
	cloud := []string{}
	if r.Tokens.CloudGenshin != "" {
		cloud = append(cloud, "genshin")
	}
	if r.Tokens.CloudZZZ != "" {
		cloud = append(cloud, "zzz")
	}
	// The label is the whole account ID: it is no credential, and only CK is
	// kept out of chats and pages.
	return Summary{Realm: r.Tokens.RealmName(), CloudConfigured: cloud, LastCheck: check, PublicQuery: public, Ref: r.Ref, Revision: r.Revision, Owner: r.Owner, AccountLabel: r.Tokens.AccountID, Roles: roles, AllowedCallers: callers, Status: r.Status, CanRefresh: r.Tokens.SToken != "" && (r.Tokens.Realm != "os" || (!r.Tokens.STokenV2 || r.Tokens.MID != "")), UpdatedAtMS: r.UpdatedAtMS}
}

func (t Tokens) RealmName() string {
	if t.Realm == "os" {
		return "os"
	}
	return "cn"
}
func (t Tokens) ValidBinding() bool {
	return slices.Contains([]string{"", "cn", "os"}, t.Realm) && t.AccountID != "" && t.LToken != "" && (t.Realm == "os" || t.CookieToken != "")
}

type CheckResult struct {
	Kind        string `json:"kind"`
	State       string `json:"state"`
	Code        string `json:"code,omitempty"`
	CheckedAtMS int64  `json:"checked_at_ms"`
}

func KnownCheckCode(code string) bool {
	return slices.Contains([]string{"", "plugin.upstream_auth_invalid", "plugin.upstream_device_required", "plugin.upstream_challenge_required", "plugin.upstream_rate_limited", "plugin.upstream_rejected", "plugin.upstream_request_invalid", "plugin.upstream_response_invalid", "plugin.upstream_unavailable", "plugin.upstream_endpoint_invalid", "plugin.account_operation_canceled", "plugin.account_operation_failed"}, code)
}
func (c *CheckResult) Valid() bool {
	return c == nil || slices.Contains([]string{"check", "refresh"}, c.Kind) && slices.Contains([]string{"available", "auth_invalid", "device_required", "challenge_required", "unavailable"}, c.State) && KnownCheckCode(c.Code) && c.CheckedAtMS > 0 && (c.State == "available") == (c.Code == "")
}
