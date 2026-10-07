// Package i18n holds every sentence sereno shows, in English and in Brazilian
// Portuguese (R5). The two catalogues are Go maps with identical key sets; a
// test fails when a key is missing from one of them or when the two texts of a
// key take different arguments.
//
// Not translated, and therefore not here: command names, flags, the plan verbs
// of R14, JSON keys, warning ids and the contents of generated files.
package i18n

import (
	"fmt"
	"sort"
	"strings"
)

// Lang is a language of the interface.
type Lang string

// The two languages of 0.1.
const (
	English    Lang = "en"
	Portuguese Lang = "pt-BR"
)

// catalogues maps each language to its messages. The maps are written once,
// in en.go and ptbr.go, and never modified.
var catalogues = map[Lang]map[string]string{
	English:    english,
	Portuguese: portuguese,
}

// Parse recognises the values of --lang and SERENO_LANG: "en" and "pt-BR", in
// any letter case.
func Parse(value string) (Lang, bool) {
	switch strings.ToLower(value) {
	case "en":
		return English, true
	case "pt-br":
		return Portuguese, true
	}
	return "", false
}

// Select applies the order of R5: the --lang flag, then SERENO_LANG, then the
// language of the operating system, otherwise English. A value that names no
// supported language is skipped, so a stray SERENO_LANG cannot break output.
func Select(flag, env, system string) Lang {
	for _, value := range []string{flag, env, system} {
		if lang, ok := Parse(value); ok {
			return lang
		}
	}
	return English
}

// Catalog translates message keys into one language.
type Catalog struct {
	lang     Lang
	messages map[string]string
}

// For returns the catalogue of a language; an unknown language gets English.
func For(lang Lang) *Catalog {
	messages, ok := catalogues[lang]
	if !ok {
		lang, messages = English, english
	}
	return &Catalog{lang: lang, messages: messages}
}

// Lang returns the language of the catalogue.
func (c *Catalog) Lang() Lang { return c.lang }

// T returns the message for key, formatted with args as fmt.Sprintf would. A
// key that does not exist comes back as "!key!" so the mistake is visible; the
// tests of this package and of internal/policy keep that from shipping.
func (c *Catalog) T(key string, args ...any) string {
	format, ok := c.messages[key]
	if !ok {
		return "!" + key + "!"
	}
	if len(args) == 0 {
		return format
	}
	return fmt.Sprintf(format, args...)
}

// Has reports whether key exists in the catalogues.
func Has(key string) bool {
	_, ok := english[key]
	return ok
}

// Keys returns every message key, sorted.
func Keys() []string {
	keys := make([]string, 0, len(english))
	for key := range english {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
