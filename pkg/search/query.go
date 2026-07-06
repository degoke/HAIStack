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
	FieldKey  string
	Direction SortDirection
}

// MatchOperator identifies how a predicate value is compared.
type MatchOperator string

const (
	OpEqual    MatchOperator = "eq"
	OpNotEqual MatchOperator = "ne"
	OpGreater  MatchOperator = "gt"
	OpLess     MatchOperator = "lt"
	OpGE       MatchOperator = "ge"
	OpLE       MatchOperator = "le"
	OpStarts   MatchOperator = "sa"
	OpEnds     MatchOperator = "eb"
	OpApprox   MatchOperator = "ap"
	OpContains MatchOperator = "contains"
	OpExact    MatchOperator = "exact"
	OpText     MatchOperator = "text"
	OpBelow    MatchOperator = "below"
	OpAbove    MatchOperator = "above"
	OpIn       MatchOperator = "in"
	OpNotIn    MatchOperator = "not-in"
	OpNot      MatchOperator = "not"
	OpIdentifier MatchOperator = "identifier"
	OpType     MatchOperator = "type"
)

// ParamClause is one resolved search parameter with OR semantics across Values.
type ParamClause struct {
	Code      string
	Modifier  string
	ParamType string
	FieldKey  string
	Values    []ValueClause
}

// ValueClause is one search value with an optional comparator prefix.
type ValueClause struct {
	Raw      string
	Prefix   string
	Operator MatchOperator
}

// ChainClause is a single-hop chained search parameter (e.g. subject.name).
type ChainClause struct {
	RefCode     string
	RefFieldKey string
	TargetType  string
	Param       ParamClause
}

// IncludeDirective requests direct include expansion for one reference parameter.
type IncludeDirective struct {
	SourceType string
	ParamCode  string
	TargetType string
}

// RevIncludeDirective requests reverse include expansion.
type RevIncludeDirective struct {
	SourceType string
	ParamCode  string
	TargetType string
}

// SummaryMode controls _summary projection behavior.
type SummaryMode string

const (
	SummaryNone SummaryMode = ""
	SummaryTrue SummaryMode = "true"
	SummaryText SummaryMode = "text"
	SummaryData SummaryMode = "data"
	SummaryCount SummaryMode = "count"
)

// Query is the normalized internal representation of a FHIR search request.
type Query struct {
	ResourceType string
	Params       []ParamClause
	Chains       []ChainClause
	Includes     []IncludeDirective
	RevIncludes  []RevIncludeDirective
	Count        int
	Offset       int
	Sort         []SortField
	Summary      SummaryMode
	Elements     []string
	FullText     string
}

// Predicate is one backend-neutral search predicate.
type Predicate struct {
	FieldKey string
	Value    string
	Operator MatchOperator
}

// ParamPlan groups OR predicates for one parameter; parameters AND together.
type ParamPlan struct {
	Code        string
	FieldKey    string
	ParamType   string
	Predicates  []Predicate
	CombineMode combineMode
}

type combineMode int

const (
	combineOr combineMode = iota
	combineAnd
)

// ChainPlan is a single-hop chained search execution stage.
type ChainPlan struct {
	RefCode     string
	RefFieldKey string
	TargetType  string
	ParamPlan   ParamPlan
}

// IncludePlan describes direct include expansion after primary search.
type IncludePlan struct {
	SourceType string
	ParamCode  string
	RefFieldKey string
	TargetType string
}

// RevIncludePlan describes reverse include expansion after primary search.
type RevIncludePlan struct {
	SourceType  string
	ParamCode   string
	RefFieldKey string
	TargetType  string
}

// Plan is a backend-neutral search execution plan.
type Plan struct {
	ResourceType string
	ParamPlans   []ParamPlan
	ChainPlans   []ChainPlan
	Includes     []IncludePlan
	RevIncludes  []RevIncludePlan
	Count        int
	Offset       int
	Sort         []SortField
	Summary      SummaryMode
	Elements     []string
	FullText     string
}
