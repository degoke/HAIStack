package search

// SortDirection is the order for one sort field.
type SortDirection int

const (
	SortAsc SortDirection = iota
	SortDesc
)

// SortField identifies one sort key in a search request.
type SortField struct {
	Code      string
	Direction SortDirection
}

// ParamClause is one resolved search parameter with OR semantics across Values.
type ParamClause struct {
	Code     string
	FieldKey string
	Values   []string
}

// Query is the normalized internal representation of a FHIR search request.
type Query struct {
	ResourceType string
	Params       []ParamClause
	Count        int
	Offset       int
	Sort         []SortField
}

// Predicate is one backend-neutral equality predicate.
type Predicate struct {
	FieldKey string
	Value    string
}

// ParamPlan groups OR predicates for one parameter; parameters AND together.
type ParamPlan struct {
	Code        string
	Predicates  []Predicate
	CombineMode combineMode
}

type combineMode int

const (
	combineOr combineMode = iota
	combineAnd
)

// Plan is a backend-neutral search execution plan.
type Plan struct {
	ResourceType string
	ParamPlans   []ParamPlan
	Count        int
	Offset       int
	Sort         []SortField
}
