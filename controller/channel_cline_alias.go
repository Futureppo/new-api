package controller

import (
	"slices"
	"strings"

	"github.com/QuantumNous/new-api/model"
)

func clineModelAlias(id string) string {
	if prefix, name, ok := strings.Cut(id, "/"); ok && prefix != "" && strings.TrimSpace(name) != "" {
		return strings.TrimSpace(name)
	}
	return id
}

func collectClineGeneratedMappings(channel *model.Channel, saved map[string]string) map[string]string {
	current := normalizeChannelModelMapping(channel)
	owned := make(map[string]string)
	for alias, target := range saved {
		if current[alias] == target && alias != target && clineModelAlias(target) == alias {
			owned[alias] = target
		}
	}
	return owned
}

// Reserve manual names/mappings and keep full IDs when aliases are ambiguous.
// The upstream catalog itself always retains full IDs for requests and ignores.
func planClineModelAliases(channel *model.Channel, upstream []string, saved map[string]string, ignored []string) ([]string, map[string]string) {
	upstream = normalizeModelNames(upstream)
	current := channel.GetModels()
	mapping := normalizeChannelModelMapping(channel)
	owned := collectClineGeneratedMappings(channel, saved)
	counts := make(map[string]int)
	for _, id := range upstream {
		// A saved alias and its upstream ID describe the same model. Counting
		// both made an edit after fetching models look like a provider collision.
		if target := mapping[id]; target != id && clineModelAlias(target) == id && slices.Contains(upstream, target) {
			continue
		}
		counts[clineModelAlias(id)]++
	}
	desired := make([]string, 0, len(upstream))
	aliases := make(map[string]string)
	for _, id := range upstream {
		alias := clineModelAlias(id)
		_, autoAlias := owned[alias]
		_, reservedAlias := mapping[alias]
		_, mappedID := mapping[id]
		// Reuse an exact mapping even if it predates ownership metadata or was
		// entered manually. A separate mapping on the full ID still takes priority.
		if alias != id && mapping[alias] == id && !mappedID {
			desired = append(desired, alias)
			aliases[alias] = id
			continue
		}
		if alias == id || counts[alias] > 1 || mappedID ||
			(!autoAlias && (slices.Contains(current, alias) || reservedAlias)) ||
			isIgnoredUpstreamModel(id, ignored) || isIgnoredUpstreamModel(alias, ignored) {
			desired = append(desired, id)
			continue
		}
		desired = append(desired, alias)
		aliases[alias] = id
	}
	return normalizeModelNames(desired), aliases
}

// Automatic sync must not fall back to prefixed IDs when a short name is
// ambiguous or reserved. Keep manual entries, and wait for an explicit mapping.
func planClineSyncModelAliases(channel *model.Channel, upstream []string, saved map[string]string, ignored []string) ([]string, map[string]string) {
	desired, aliases := planClineModelAliases(channel, upstream, saved, ignored)
	return filterClineSyncModelNames(desired, aliases), aliases
}

func filterClineSyncModelNames(names []string, aliases map[string]string) []string {
	shortNames := make([]string, 0, len(names))
	for _, name := range names {
		if clineModelAlias(name) == name || aliases[name] != "" {
			shortNames = append(shortNames, name)
		}
	}
	return shortNames
}

// Normalize explicit channel writes too, including when free sync is disabled.
// Only generated mappings whose values are unchanged remain backend-owned.
func normalizeClineChannelModels(channel *model.Channel) (bool, error) {
	settings := channel.GetOtherSettings()
	next, aliases := planClineModelAliases(channel, channel.GetModels(), settings.ClineModelGeneratedMappings, nil)
	mapping := cloneModelMapping(normalizeChannelModelMapping(channel))
	generated := collectClineGeneratedMappings(channel, settings.ClineModelGeneratedMappings)
	for alias := range settings.ClineModelGeneratedMappings {
		if _, owned := generated[alias]; !owned {
			settings.ClineFreeModelManagedModels = subtractModelNames(settings.ClineFreeModelManagedModels, []string{alias})
		}
	}
	for alias, target := range aliases {
		_, alreadyMapped := mapping[alias]
		_, alreadyOwned := generated[alias]
		mapping[alias] = target
		if !alreadyMapped || alreadyOwned {
			generated[alias] = target
			if slices.Contains(settings.ClineFreeModelManagedModels, target) {
				settings.ClineFreeModelManagedModels = mergeModelNames(settings.ClineFreeModelManagedModels, []string{alias})
			}
		}
	}
	for alias := range generated {
		if !slices.Contains(next, alias) {
			delete(mapping, alias)
			delete(generated, alias)
		}
	}
	mappingChanged, err := setChannelModelMapping(channel, mapping)
	if err != nil {
		return false, err
	}
	changed := mappingChanged || !slices.Equal(normalizeModelNames(channel.GetModels()), next)
	channel.Models = strings.Join(next, ",")
	settings.ClineModelGeneratedMappings = filterModelMappingsBySources(generated, next)
	settings.ClineFreeModelManagedModels = intersectModelNames(settings.ClineFreeModelManagedModels, next)
	if changed {
		settings.ClineFreeModelPendingMappings = nil
		settings.UpstreamModelUpdateLastDetectedModels = nil
		settings.UpstreamModelUpdateLastRemovedModels = nil
		settings.UpstreamModelUpdateLastCheckTime = 0
	}
	channel.SetOtherSettings(settings)
	return changed, nil
}
