package common

import (
	"strings"
	"unicode/utf8"
)

// A trie provides the same leftmost-longest, single-pass semantics for both
// complete strings and incremental text. Replacement text is never rescanned.
type keywordMatcher struct {
	children map[byte]*keywordMatcher
	terminal bool
	value    string
}

func newKeywordMatcher(rules map[string]string) *keywordMatcher {
	root := &keywordMatcher{}
	for key, replacement := range rules {
		if key == "" {
			continue
		}
		node := root
		for i := 0; i < len(key); i++ {
			if node.children == nil {
				node.children = make(map[byte]*keywordMatcher)
			}
			if node.children[key[i]] == nil {
				node.children[key[i]] = &keywordMatcher{}
			}
			node = node.children[key[i]]
		}
		node.terminal, node.value = true, replacement
	}
	return root
}

// next returns zero consumed bytes only when another delta could extend the
// current prefix to a longer match. JSON text fragments are valid UTF-8 strings.
func (m *keywordMatcher) next(text string, final bool) (int, string) {
	node, matched, replacement := m, 0, ""
	for i := 0; i < len(text); i++ {
		node = node.children[text[i]]
		if node == nil {
			break
		}
		if node.terminal {
			matched, replacement = i+1, node.value
		}
		if i+1 == len(text) && !final && len(node.children) != 0 {
			return 0, ""
		}
	}
	if matched != 0 {
		return matched, replacement
	}
	_, size := utf8.DecodeRuneInString(text)
	return size, text[:size]
}

func (m *keywordMatcher) replace(text string) string {
	var out strings.Builder
	for len(text) != 0 {
		n, replacement := m.next(text, true)
		out.WriteString(replacement)
		text = text[n:]
	}
	return out.String()
}
