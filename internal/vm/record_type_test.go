package vm

import "testing"

func TestDefrecordBehavior(t *testing.T) {
	runner := MustNewRunner()
	if _, err := runner.Run(`(defrecord Point [x y]) (def p (->Point 3 4))`); err != nil {
		t.Fatalf("define Point: %v", err)
	}

	assertRunResult(t, runner, `[
		(->Point 1 2)
		(map->Point {:x 10 :y 20})
		(map->Point {:x 5})
		(map->Point {:x 1 :y 2 :z 3})
		(:x p)
		(get p :y)
		(get p :z :not-found)
		(assoc p :x 100)
		(assoc p :z 5)
		(dissoc (assoc p :z 5) :z)
		(count p)
		(count (assoc p :z 5))
		(keys p)
		(vals p)
		(type-name p)
		(record? p)
		(record? {:x 3 :y 4})]`,
		`[{:x 1, :y 2} {:x 10, :y 20} {:x 5, :y nil} {:x 1, :y 2, :z 3} 3 4 :not-found {:x 100, :y 4} {:x 3, :y 4, :z 5} {:x 3, :y 4} 2 3 (:x :y) (3 4) "Point" true false]`)
}

func TestNamedTypeBindingsAreProtected(t *testing.T) {
	runner := MustNewRunner()
	if _, err := runner.Run(`(defrecord Point [x y]) (deftype Counter [value])`); err != nil {
		t.Fatalf("define named types: %v", err)
	}
	assertRunResult(t, runner,
		`[(var? Point) (record-def? Point) (var? Counter) (type-def? Counter)]`,
		`[false true false true]`)

	for _, source := range []string{
		`(def Point 42)`,
		`(defmacro Point [] 1)`,
		`(defmulti Point identity)`,
		`(def Counter 42)`,
	} {
		assertRunErrorContains(t, runner, source, "cannot redefine protected type name")
	}
}

func TestDefrecordInlineProtocolDispatch(t *testing.T) {
	runner := MustNewRunner()
	if _, err := runner.Run(`
		(defprotocol IShape (area [this]) (perimeter [this]))
		(defrecord Circle [r]
			IShape
			(area [this] (* 3.14 (:r this) (:r this)))
			(perimeter [this] (* 2 3.14 (:r this))))
		(defrecord Rectangle [w h]
			IShape
			(area [this] (* (:w this) (:h this)))
			(perimeter [this] (* 2 (+ (:w this) (:h this)))))`); err != nil {
		t.Fatalf("define shapes: %v", err)
	}
	assertRunResult(t, runner,
		`[(area (->Circle 2)) (perimeter (->Circle 2)) (area (->Rectangle 3 4)) (perimeter (->Rectangle 3 4))]`,
		`[12.56 12.56 12 14]`)
}

func TestDeftypeBehavior(t *testing.T) {
	runner := MustNewRunner()
	if _, err := runner.Run(`
		(defprotocol IPoint (move [this dx dy]) (legacy-x [this]))
		(deftype Point [x y]
			IPoint
			(move [_ dx dy] (->Point (+ x dx) (+ y dy)))
			(legacy-x [this] (get-field this :x)))
		(def p (->Point 10 20))`); err != nil {
		t.Fatalf("define Point: %v", err)
	}
	assertRunResult(t, runner, `[
		(type-name p)
		(get-field p :x)
		(get-field p :y)
		(let [moved (move p 5 -3)] [(get-field moved :x) (get-field moved :y)])
		(legacy-x p)]`,
		`["Point" 10 20 [15 17] 10]`)
}

func TestNamedTypesUseNamespaceQualifiedIdentity(t *testing.T) {
	assertRunResult(t, MustNewRunner(), `
		(do
			(ns identity.protocol)
			(defprotocol Tagged (tag [value]))

			(ns identity.a)
			(defrecord Item [value] identity.protocol/Tagged (tag [_] :a))
			(deftype Opaque [value])
			(def item-a (->Item 1))
			(def opaque-a (->Opaque 1))
			(defprotocol Same (same [value]))
			(def protocol-a Same)

			(ns identity.b)
			(defrecord Item [value] identity.protocol/Tagged (tag [_] :b))
			(deftype Opaque [value])
			(def item-b (->Item 2))
			(def opaque-b (->Opaque 1))
			(defprotocol Same (same [value]))

			[(identity.protocol/tag identity.a/item-a)
			 (identity.protocol/tag item-b)
			 (= identity.a/opaque-a opaque-b)
			 (= identity.a/protocol-a Same)
			 (type-name identity.a/item-a)
			 (type-name item-b)
			 (type-name identity.a/opaque-a)
			 (type-name opaque-b)])`,
		`[:a :b false false "Item" "Item" "Opaque" "Opaque"]`)
}

func TestExtendTypeUsesQualifiedLexicalIdentity(t *testing.T) {
	assertRunResult(t, MustNewRunner(), `
		(do
			(ns extension.protocol)
			(defprotocol Tagged (tag [value]))
			(ns extension.a)
			(deftype Item [value])
			(extend-type Item extension.protocol/Tagged (tag [_] :a))
			(def item-a (->Item 1))
			(ns extension.b)
			(deftype Item [value])
			(extend-type Item extension.protocol/Tagged (tag [_] :b))
			[(extension.protocol/tag extension.a/item-a)
			 (extension.protocol/tag (->Item 2))])`,
		`[:a :b]`)

	assertRunResult(t, MustNewRunner(), `
		(do
			(ns extension.helper)
			(defn helper-id [value] value)
			(ns extension.lexical.protocol)
			(defprotocol Identified (identify [value]))
			(ns extension.lexical.target)
			(deftype Item [value])
			(defn install []
				(extend-type Item extension.lexical.protocol/Identified
					(identify [value] (extension.helper/helper-id value))))
			(ns extension.lexical.caller)
			(extension.lexical.target/install)
			(let [item (extension.lexical.target/->Item 7)]
				(= (extension.lexical.protocol/identify item) item)))`,
		`true`)
}

func TestDefrecordMapAndNilSemantics(t *testing.T) {
	runner := MustNewRunner()
	if _, err := runner.Run(`
		(defrecord User [name])
		(defrecord LeftRecord [left])
		(defrecord RightRecord [right])`); err != nil {
		t.Fatalf("define records: %v", err)
	}
	assertRunResult(t, runner, `
		(let [record (assoc (->User "Ada") :age 42)
		      plain {:name "Ada" :age 42}
		      dissoced (dissoc (->User "Ada") :name)
		      from-map (map->User {})]
		  [(= record plain)
		   (= plain record)
		   (= :record (get {record :record} plain))
		   (= :map (get {plain :map} record))
		   (nil? (get dissoced :name :missing))
		   (contains? dissoced :name)
		   (= dissoced {:name nil})
		   (= from-map {:name nil})
		   (= :nil-record (get {from-map :nil-record} {:name nil}))
		   (not= (->LeftRecord nil) (->RightRecord 1))])`,
		`[true true true true true true true true true true]`)
}
