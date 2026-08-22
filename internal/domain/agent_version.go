package domain

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
)

const AgentManifestSchemaVersion = 1

type AgentCapabilityType string

const (
	CapabilityModel    AgentCapabilityType = "MODEL"
	CapabilityTool     AgentCapabilityType = "TOOL"
	CapabilitySkill    AgentCapabilityType = "SKILL"
	CapabilitySubagent AgentCapabilityType = "SUBAGENT"
)

type AgentLimits struct {
	MaxExecutionTimeSeconds *int64 `json:"max_execution_time_seconds,omitempty"`
	MaxBudgetUSDMicrounits  *int64 `json:"max_budget_usd_microunits,omitempty"`
	MaxLLMTokens            *int64 `json:"max_llm_tokens,omitempty"`
	MaxToolCalls            *int64 `json:"max_tool_calls,omitempty"`
	MaxToolCallsPerMinute   *int64 `json:"max_tool_calls_per_minute,omitempty"`
	MaxActiveChildren       *int64 `json:"max_active_children,omitempty"`
	MaxTotalChildren        *int64 `json:"max_total_children,omitempty"`
	MaxDelegationDepth      *int64 `json:"max_delegation_depth,omitempty"`
}

type AgentManifest struct {
	SchemaVersion int         `json:"schema_version"`
	Image         string      `json:"image"`
	Models        []string    `json:"models"`
	Tools         []string    `json:"tools"`
	Skills        []string    `json:"skills"`
	Subagents     []string    `json:"subagents"`
	Limits        AgentLimits `json:"limits"`
}

type AgentVersion struct {
	ID, TenantID, AgentID, CreatedBy ID
	ContentDigest, ImageDigest       string
	Manifest                         AgentManifest
	CreatedAt                        time.Time
}

type AgentCapability struct {
	ID, TenantID, AgentID, AgentVersionID ID
	Type                                  AgentCapabilityType
	Identifier                            string
	CreatedAt                             time.Time
}

func NewAgentManifest(image string, models, tools, skills, subagents []string, limits AgentLimits) (AgentManifest, error) {
	manifest := AgentManifest{SchemaVersion: AgentManifestSchemaVersion, Image: image, Models: append([]string(nil), models...), Tools: append([]string(nil), tools...), Skills: append([]string(nil), skills...), Subagents: append([]string(nil), subagents...), Limits: limits}
	manifest.Models = normalizeStrings(manifest.Models)
	manifest.Tools = normalizeStrings(manifest.Tools)
	manifest.Skills = normalizeStrings(manifest.Skills)
	manifest.Subagents = normalizeStrings(manifest.Subagents)
	if err := manifest.Validate(); err != nil {
		return AgentManifest{}, err
	}
	return manifest, nil
}

func (manifest AgentManifest) Validate() error {
	if manifest.SchemaVersion != AgentManifestSchemaVersion {
		return errors.New("unsupported agent manifest schema version")
	}
	if _, err := ImageDigest(manifest.Image); err != nil {
		return err
	}
	if manifest.Models == nil || manifest.Tools == nil || manifest.Skills == nil || manifest.Subagents == nil {
		return errors.New("agent manifest declaration arrays are required")
	}
	for _, declaration := range []struct {
		name   string
		values []string
		maxLen int
		id     bool
	}{{"models", manifest.Models, 128, true}, {"tools", manifest.Tools, 128, true}, {"skills", manifest.Skills, 1024, false}, {"subagents", manifest.Subagents, 128, true}} {
		if len(declaration.values) > 100 {
			return fmt.Errorf("agent manifest %s exceeds 100 declarations", declaration.name)
		}
		previous := ""
		for _, value := range declaration.values {
			if !validDeclaration(value, declaration.maxLen, declaration.id) {
				return fmt.Errorf("agent manifest %s contains an invalid declaration", declaration.name)
			}
			if value <= previous {
				return fmt.Errorf("agent manifest %s must be sorted and unique", declaration.name)
			}
			previous = value
		}
	}
	return manifest.Limits.Validate()
}

func (limits AgentLimits) Validate() error {
	bounded := []struct {
		value *int64
		max   int64
		name  string
		zero  bool
	}{
		{limits.MaxExecutionTimeSeconds, 604800, "max_execution_time_seconds", false},
		{limits.MaxBudgetUSDMicrounits, int64(^uint64(0) >> 1), "max_budget_usd_microunits", true},
		{limits.MaxLLMTokens, int64(^uint64(0) >> 1), "max_llm_tokens", true},
		{limits.MaxToolCalls, int64(^uint64(0) >> 1), "max_tool_calls", true},
		{limits.MaxToolCallsPerMinute, int64(^uint64(0) >> 1), "max_tool_calls_per_minute", true},
		{limits.MaxActiveChildren, int64(^uint64(0) >> 1), "max_active_children", true},
		{limits.MaxTotalChildren, int64(^uint64(0) >> 1), "max_total_children", true},
		{limits.MaxDelegationDepth, int64(^uint64(0) >> 1), "max_delegation_depth", true},
	}
	for _, limit := range bounded {
		if limit.value != nil && (*limit.value < 0 || !limit.zero && *limit.value == 0 || *limit.value > limit.max) {
			return fmt.Errorf("agent limit %s is outside its allowed range", limit.name)
		}
	}
	if limits.MaxActiveChildren != nil && limits.MaxTotalChildren != nil && *limits.MaxActiveChildren > *limits.MaxTotalChildren {
		return errors.New("max_active_children cannot exceed max_total_children")
	}
	return nil
}

func (manifest AgentManifest) CanonicalJSON() ([]byte, error) {
	if err := manifest.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(manifest)
}

func ParseAgentManifest(data []byte) (AgentManifest, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil || len(fields) != 7 {
		return AgentManifest{}, errors.New("agent manifest must contain every schema-v1 field")
	}
	for _, required := range []string{"schema_version", "image", "models", "tools", "skills", "subagents", "limits"} {
		if _, ok := fields[required]; !ok {
			return AgentManifest{}, errors.New("agent manifest must contain every schema-v1 field")
		}
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var manifest AgentManifest
	if err := decoder.Decode(&manifest); err != nil {
		return AgentManifest{}, errors.New("invalid agent manifest JSON")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return AgentManifest{}, errors.New("agent manifest contains trailing JSON")
	}
	if err := manifest.Validate(); err != nil {
		return AgentManifest{}, err
	}
	return manifest, nil
}

func (manifest AgentManifest) ContentDigest() (string, error) {
	canonical, err := manifest.CanonicalJSON()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("sha256:%x", sha256.Sum256(canonical)), nil
}

func ImageDigest(image string) (string, error) {
	if len(image) < 73 || len(image) > 1024 || strings.TrimSpace(image) != image {
		return "", errors.New("agent image must be a bounded digest-pinned reference")
	}
	index := strings.LastIndex(image, "@sha256:")
	if index < 1 || index+8+64 != len(image) {
		return "", errors.New("agent image must end in a sha256 digest")
	}
	for _, character := range image[:index] {
		if character < 0x21 || character > 0x7e {
			return "", errors.New("agent image repository is invalid")
		}
	}
	digest := image[index+1:]
	for _, character := range digest[len("sha256:"):] {
		if character < '0' || character > '9' && character < 'a' || character > 'f' {
			return "", errors.New("agent image digest must be lowercase hexadecimal")
		}
	}
	return digest, nil
}

func (version AgentVersion) Validate() error {
	if version.ID.IsZero() || version.TenantID.IsZero() || version.AgentID.IsZero() || version.CreatedBy.IsZero() {
		return errors.New("agent version identifiers must be set")
	}
	if _, err := RequireUTC(version.CreatedAt); err != nil || version.CreatedAt.IsZero() {
		return errors.New("agent version creation time must be a non-zero UTC timestamp")
	}
	digest, err := version.Manifest.ContentDigest()
	if err != nil || digest != version.ContentDigest {
		return errors.New("agent version content digest does not match its canonical manifest")
	}
	imageDigest, err := ImageDigest(version.Manifest.Image)
	if err != nil || imageDigest != version.ImageDigest {
		return errors.New("agent version image digest does not match its manifest")
	}
	return nil
}

func (version AgentVersion) Capabilities(ids []ID) ([]AgentCapability, error) {
	count := len(version.Manifest.Models) + len(version.Manifest.Tools) + len(version.Manifest.Skills) + len(version.Manifest.Subagents)
	if len(ids) != count {
		return nil, errors.New("capability ID count does not match manifest")
	}
	result := make([]AgentCapability, 0, count)
	index := 0
	for _, group := range []struct {
		typeName AgentCapabilityType
		values   []string
	}{{CapabilityModel, version.Manifest.Models}, {CapabilityTool, version.Manifest.Tools}, {CapabilitySkill, version.Manifest.Skills}, {CapabilitySubagent, version.Manifest.Subagents}} {
		for _, identifier := range group.values {
			result = append(result, AgentCapability{ID: ids[index], TenantID: version.TenantID, AgentID: version.AgentID, AgentVersionID: version.ID, Type: group.typeName, Identifier: identifier, CreatedAt: version.CreatedAt})
			index++
		}
	}
	return result, nil
}

func normalizeStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	sort.Strings(values)
	return values
}

func validDeclaration(value string, maxLength int, identifier bool) bool {
	if len(value) < 1 || len(value) > maxLength || strings.TrimSpace(value) != value {
		return false
	}
	for index, character := range value {
		if identifier {
			if index == 0 && !asciiAlphanumeric(character) || index > 0 && !asciiAlphanumeric(character) && character != '.' && character != '_' && character != ':' && character != '-' {
				return false
			}
			continue
		}
		if character < 0x21 || character == 0x7f {
			return false
		}
	}
	return true
}

func asciiAlphanumeric(character rune) bool {
	return character >= 'A' && character <= 'Z' || character >= 'a' && character <= 'z' || character >= '0' && character <= '9'
}
