package mistralconsole

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
)

const (
	boraFunctionAliasPrefix   = "mc_fn_"
	boraFunctionNameMaxLength = 64
	boraFunctionHashLength    = 16
)

type boraFunctionNameMapper struct {
	originalToAlias map[string]string
	aliasToOriginal map[string]string
}

func newBoraFunctionNameMapper() *boraFunctionNameMapper {
	return &boraFunctionNameMapper{
		originalToAlias: make(map[string]string),
		aliasToOriginal: make(map[string]string),
	}
}

func (mapper *boraFunctionNameMapper) alias(original string) string {
	if alias, exists := mapper.originalToAlias[original]; exists {
		return alias
	}

	for attempt := 0; ; attempt++ {
		hashInput := original
		if attempt > 0 {
			hashInput += "#" + strconv.Itoa(attempt)
		}
		alias := buildBoraFunctionAlias(original, hashInput)
		if existing, exists := mapper.aliasToOriginal[alias]; exists && existing != original {
			continue
		}
		mapper.originalToAlias[original] = alias
		mapper.aliasToOriginal[alias] = original
		return alias
	}
}

func (mapper *boraFunctionNameMapper) original(alias string) string {
	if mapper == nil {
		return alias
	}
	if original, exists := mapper.aliasToOriginal[alias]; exists {
		return original
	}
	return alias
}

func buildBoraFunctionAlias(original string, hashInput string) string {
	readable := sanitizeBoraFunctionName(original)
	digest := sha256.Sum256([]byte(hashInput))
	hash := hex.EncodeToString(digest[:])[:boraFunctionHashLength]
	readableLimit := boraFunctionNameMaxLength - len(boraFunctionAliasPrefix) - 1 - len(hash)
	if len(readable) > readableLimit {
		readable = readable[:readableLimit]
	}
	return boraFunctionAliasPrefix + readable + "_" + hash
}

func sanitizeBoraFunctionName(name string) string {
	var builder strings.Builder
	builder.Grow(len(name))
	lastUnderscore := false
	for index := 0; index < len(name); index++ {
		char := name[index]
		valid := (char >= 'a' && char <= 'z') ||
			(char >= 'A' && char <= 'Z') ||
			(char >= '0' && char <= '9') ||
			char == '_' || char == '-'
		if valid {
			builder.WriteByte(char)
			lastUnderscore = false
			continue
		}
		if !lastUnderscore {
			builder.WriteByte('_')
			lastUnderscore = true
		}
	}
	readable := strings.Trim(builder.String(), "_-")
	if readable == "" {
		return "tool"
	}
	return readable
}
