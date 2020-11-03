package schemas

// Deployment ..
type Deployment struct {
	ID              int
	RefKind         RefKind
	RefName         string
	AuthorEmail     string
	Timestamp       float64
	DurationSeconds float64
	CommitShortID   string
	Status          string
}
