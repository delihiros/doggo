package vm

import (
	"strings"
	"sync"
)

var (
	defaultBuiltinCatalogOnce sync.Once
	defaultBuiltinCatalog     *BuiltinCatalog
)

func buildDefaultBuiltinCatalog() *BuiltinCatalog {
	return MustBuiltinCatalog(
		builtinsAgent,
		builtinsAtom,
		builtinsBinding,
		builtinsChannel,
		builtinsColl,
		builtinsCollExtra,
		builtinsCollExtraCtx,
		builtinsCollHOF,
		builtinsCollTransient,
		builtinsCompare,
		builtinsCore,
		builtinsCoreExt,
		privateBuiltinNamespaceWithNames(builtinsCSV, map[string]string{
			"csv-read-string*":  csvReadStringFunction,
			"csv-read*":         csvReadFunction,
			"csv-write-string*": csvWriteStringFunction,
			"csv-write*":        csvWriteFunction,
		}),
		withBuiltinOwners(builtinsData, map[string]string{"data-diff*": "doggo.data/diff"}),
		withBuiltinOwners(builtinsDatafy, map[string]string{"datafy-value*": "doggo.datafy/datafy"}),
		privateBuiltinNamespace(builtinsEDN, "doggo.edn", "edn-"),
		builtinsException,
		privateBuiltinNamespaceWithNames(builtinsInstant, map[string]string{
			"parse-timestamp*": "doggo.instant/parse-timestamp",
			"time-format*":     "doggo.time/format",
		}),
		privateBuiltinNamespaceWithNames(builtinsHTTP, map[string]string{
			"http-request*": httpPublicRequestName,
		}),
		withBuiltinOwners(builtinsIO, map[string]string{
			"slurp": "doggo.io/slurp",
			"spit":  "doggo.io/spit",
		}),
		privateBuiltinNamespace(builtinsIOPath, "doggo.io", "io-"),
		privateBuiltinNamespace(builtinsJSON, "doggo.json", "json-"),
		builtinsMath,
		privateBuiltinNamespace(builtinsMathExtra, "doggo.math", "math-"),
		builtinsMetadata,
		builtinsMultimethod,
		privateBuiltinNamespace(builtinsOS, "doggo.os", "os-"),
		privateBuiltinNamespace(builtinsProcess, "doggo.process", "process-"),
		withBuiltinOwners(protocolBuiltins, map[string]string{
			"defprotocol*":           "doggo.core/defprotocol",
			"extend-type*":           "doggo.core/extend-type",
			"find-protocol-method*":  "doggo.core/defprotocol",
			"invoke-protocol-method": "doggo.core/defprotocol",
		}),
		additionalProtocolBuiltins,
		recordTypeBuiltins,
		builtinsRegex,
		privateBuiltinNamespace(builtinsSchema, "doggo.schema", ""),
		privateBuiltinNamespace(builtinsSetOps, "doggo.set", ""),
		builtinsSortCtx,
		builtinsSTM,
		privateNamedBuiltins(builtinsString, "doggo.string", domainStringBuiltinNames...),
		privateNamedBuiltins(builtinsStringExtra, "doggo.string", domainStringBuiltinNames...),
		builtinsSymbolExtra,
		privateBuiltinNamespace(builtinsTime, "doggo.time", "time-"),
		builtinsTransducer,
		builtinsUtilCtx,
		privateBuiltinNamespaceWithNames(builtinsURL, map[string]string{
			"url-parse*":        urlParseFunction,
			"url-format*":       urlFormatFunction,
			"url-resolve*":      urlResolveFunction,
			"url-encode-query*": urlEncodeQueryFunction,
			"url-decode-query*": urlDecodeQueryFunction,
		}),
		privateBuiltinNamespaceWithNames(builtinsUUID, map[string]string{
			"uuid-parse*":     uuidParseFunction,
			"uuid-random*":    uuidRandomFunction,
			"uuid-predicate*": uuidPredicateFunction,
		}),
		privateBuiltinNamespaceWithNames(builtinsXML, map[string]string{
			"xml-parse-string*": "doggo.xml/read-string",
			"xml-emit-string*":  "doggo.xml/write-string",
		}),
	)
}

var domainStringBuiltinNames = []string{
	"split", "join", "trim", "triml", "trimr", "split-lines",
	"upper-case", "lower-case", "capitalize", "blank?",
	"starts-with?", "ends-with?", "includes?", "index-of", "last-index-of",
	"replace", "replace-first", "escape",
}

func privateBuiltinNamespace(definitions []BuiltinDefinition, namespace, prefix string) []BuiltinDefinition {
	private := make([]BuiltinDefinition, len(definitions))
	copy(private, definitions)
	for index := range private {
		private[index].Visibility = BuiltinPrivate
		name := strings.TrimSuffix(strings.TrimPrefix(private[index].Name, prefix), "*")
		private[index].ErrorFunction = namespace + "/" + name
	}
	return private
}

func privateBuiltinNamespaceWithNames(definitions []BuiltinDefinition, owners map[string]string) []BuiltinDefinition {
	private := withBuiltinOwners(definitions, owners)
	for index := range private {
		private[index].Visibility = BuiltinPrivate
	}
	return private
}

func withBuiltinOwners(definitions []BuiltinDefinition, owners map[string]string) []BuiltinDefinition {
	owned := make([]BuiltinDefinition, len(definitions))
	copy(owned, definitions)
	for index := range owned {
		if function, ok := owners[owned[index].Name]; ok {
			owned[index].ErrorFunction = function
		}
	}
	return owned
}

func privateNamedBuiltins(definitions []BuiltinDefinition, namespace string, names ...string) []BuiltinDefinition {
	selected := make(map[string]struct{}, len(names))
	for _, name := range names {
		selected[name] = struct{}{}
	}
	private := make([]BuiltinDefinition, len(definitions))
	copy(private, definitions)
	for index := range private {
		if _, ok := selected[private[index].Name]; ok {
			private[index].Visibility = BuiltinPrivate
			private[index].ErrorFunction = namespace + "/" + private[index].Name
		}
	}
	return private
}

func DefaultBuiltinCatalog() *BuiltinCatalog {
	defaultBuiltinCatalogOnce.Do(func() {
		defaultBuiltinCatalog = buildDefaultBuiltinCatalog()
	})
	return defaultBuiltinCatalog
}
