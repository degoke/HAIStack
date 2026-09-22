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
	OpEqual      MatchOperator = "eq"
	OpNotEqual   MatchOperator = "ne"
	OpGreater    MatchOperator = "gt"
	OpLess       MatchOperator = "lt"
	OpGE         MatchOperator = "ge"
	OpLE         MatchOperator = "le"
	OpStarts     MatchOperator = "sa"
	OpEnds       MatchOperator = "eb"
	OpApprox     MatchOperator = "ap"
	OpContains   MatchOperator = "contains"
	OpExact      MatchOperator = "exact"
	OpText       MatchOperator = "text"
	OpBelow      MatchOperator = "below"
	OpAbove      MatchOperator = "above"
	OpIn         MatchOperator = "in"
	OpNotIn      MatchOperator = "not-in"
	OpNot        MatchOperator = "not"
	OpIdentifier MatchOperator = "identifier"
	OpType       MatchOperator = "type"
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

// ChainClause is a chained search parameter (e.g. subject.name or subject.organization.name).
// Nested is set for multi-hop chains; Param is the terminal predicate on the last hop.
type ChainClause struct {
	RefCode     string
	RefFieldKey string
	TargetType  string
	Param       ParamClause
	Nested      *ChainClause
}

// HasClause is a reverse-chained _has search (e.g. _has:Observation:subject:code=8867-4).
type HasClause struct {
	SourceType  string
	RefCode     string
	RefFieldKey string
	Param       ParamClause
	Chain       *ChainClause
	Nested      *HasClause
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
	SummaryNone  SummaryMode = ""
	SummaryFalse SummaryMode = "false"
	SummaryTrue  SummaryMode = "true"
	SummaryText  SummaryMode = "text"
	SummaryData  SummaryMode = "data"
	SummaryCount SummaryMode = "count"
)

// Query is the normalized internal representation of a FHIR search request.
type Query struct {
	ResourceType string
	Params       []ParamClause
	Chains       []ChainClause
	Has          []HasClause
	Includes     []IncludeDirective
	RevIncludes  []RevIncludeDirective
	Count        int
	CountSet     bool
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

// ChainPlan is a chained search execution stage. Nested walks one more hop.
type ChainPlan struct {
	RefCode     string
	RefFieldKey string
	TargetType  string
	ParamPlan   ParamPlan
	Nested      *ChainPlan
}

// HasPlan is a reverse-chained _has execution stage.
type HasPlan struct {
	SourceType  string
	RefCode     string
	RefFieldKey string
	ParamPlan   ParamPlan
	ChainPlan   *ChainPlan
	Nested      *HasPlan
}

// IncludePlan describes direct include expansion after primary search.
type IncludePlan struct {
	SourceType  string
	ParamCode   string
	RefFieldKey string
	TargetType  string
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
	HasPlans     []HasPlan
	Includes     []IncludePlan
	RevIncludes  []RevIncludePlan
	Count        int
	Offset       int
	Sort         []SortField
	Summary      SummaryMode
	Elements     []string
	FullText     string
}
