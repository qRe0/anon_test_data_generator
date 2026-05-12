package pii

// ColumnSample holds sampled values for a single database column.
type ColumnSample struct {
	TableName  string
	ColumnName string
	Values     []string
}

// PiiLabel describes a detected PII type for a column.
type PiiLabel struct {
	EntityType string
	Score      float64
	Generator  string // mapped generator name from the registry
}

// PresidioEntity is a single entity detected by Presidio in a text sample.
type PresidioEntity struct {
	EntityType string  `json:"entity_type"`
	Start      int     `json:"start"`
	End        int     `json:"end"`
	Score      float64 `json:"score"`
}

// PresidioRequest is the JSON body sent to the Presidio /analyze endpoint.
type PresidioRequest struct {
	Text     string `json:"text"`
	Language string `json:"language"`
}

// EntityMapping maps Presidio entity types to internal generator names.
// Keys are Presidio entity_type values; values are generator names.
// An empty value means no generator is mapped (the column is ignored).
var EntityMapping = map[string]string{
	"PERSON":        "person.name",
	"EMAIL_ADDRESS": "internet.email",
	"PHONE_NUMBER":  "phone.mobile",
	"CREDIT_CARD":   "finance.credit_card",
	"DATE_TIME":     "time.timestamp",
}
