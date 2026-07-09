package routing

import (
	"sort"

	"keydeck.local/feasibilitylab/internal/engineruntime"
)

func Select(req Requirements, candidates []Candidate) (Plan, error) {
	req = normalizeRequirements(req)
	if req.TaskID == "" || req.SessionID == "" || ((req.HandoffPackageID == "") != (req.HandoffPackageSHA256 == "")) {
		return Plan{}, ErrInvalidRequirements
	}

	normalized := make([]Candidate, 0, len(candidates))
	for _, c := range candidates {
		c = normalizeCandidate(c)
		if c.EngineID == "" || c.ProviderID == "" {
			continue
		}
		normalized = append(normalized, c)
	}
	sort.Slice(normalized, func(i, j int) bool {
		if normalized[i].EngineID != normalized[j].EngineID {
			return normalized[i].EngineID < normalized[j].EngineID
		}
		if normalized[i].ProviderID != normalized[j].ProviderID {
			return normalized[i].ProviderID < normalized[j].ProviderID
		}
		return digest(normalized[i]) < digest(normalized[j])
	})
	candidateSetSHA := digest(normalized)

	excludedEngines := stringSet(req.ExcludedEngineIDs)
	excludedProviders := stringSet(req.ExcludedProviderIDs)
	qualified := make([]Candidate, 0, len(normalized))
	for _, c := range normalized {
		if !c.Available || c.Health != engineruntime.HealthHealthy {
			continue
		}
		if excludedEngines[c.EngineID] || excludedProviders[c.ProviderID] {
			continue
		}
		if !hasCapabilities(c.Capabilities, req.RequiredCapabilities) {
			continue
		}
		qualified = append(qualified, c)
	}
	if len(qualified) == 0 {
		return Plan{}, ErrNoQualifiedRoute
	}

	sort.SliceStable(qualified, func(i, j int) bool {
		if qualified[i].EvidenceScore != qualified[j].EvidenceScore {
			return qualified[i].EvidenceScore > qualified[j].EvidenceScore
		}
		if qualified[i].EngineID != qualified[j].EngineID {
			return qualified[i].EngineID < qualified[j].EngineID
		}
		return qualified[i].ProviderID < qualified[j].ProviderID
	})
	selected := qualified[0]
	p := Plan{
		Version:               1,
		TaskID:                req.TaskID,
		SessionID:             req.SessionID,
		HandoffPackageID:      req.HandoffPackageID,
		HandoffPackageSHA256:  req.HandoffPackageSHA256,
		RequiredCapabilities:  req.RequiredCapabilities,
		SelectedEngineID:      selected.EngineID,
		SelectedProviderID:    selected.ProviderID,
		SelectedEvidenceScore: selected.EvidenceScore,
		SelectedEvidenceRefs:  selected.EvidenceRefs,
		CandidateSetSHA256:    candidateSetSHA,
	}
	p.RouteSHA256 = planDigest(p)
	p.RouteID = "route-" + p.RouteSHA256[:20]
	return p, nil
}

func Validate(plan Plan, req Requirements, candidates []Candidate) error {
	if plan.Version != 1 || plan.RouteID == "" || plan.RouteSHA256 == "" || plan.RouteSHA256 != planDigest(plan) || plan.RouteID != "route-"+plan.RouteSHA256[:20] {
		return ErrInvalidPlan
	}
	expected, err := Select(req, candidates)
	if err != nil {
		return err
	}
	if expected.RouteSHA256 != plan.RouteSHA256 || expected.RouteID != plan.RouteID {
		return ErrInvalidPlan
	}
	return nil
}

func hasCapabilities(have, need []engineruntime.Capability) bool {
	set := map[engineruntime.Capability]bool{}
	for _, c := range have {
		set[c] = true
	}
	for _, c := range need {
		if !set[c] {
			return false
		}
	}
	return true
}
func stringSet(v []string) map[string]bool {
	out := map[string]bool{}
	for _, s := range v {
		out[s] = true
	}
	return out
}
