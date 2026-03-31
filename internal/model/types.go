package model

// ---------------------------------------------------------------------------
// Core entities (nodes in the graph)
// ---------------------------------------------------------------------------

// Person is a real individual who may appear as a jury member, a talent/creator,
// or both across different commissions. The same person can have multiple
// creator identities (channel names, pseudonyms).
type Person struct {
	ID       string   `json:"id"`                 // slug, e.g. "benjamin-bonnet"
	FullName string   `json:"full_name"`           // canonical real name
	Aliases  []string `json:"aliases,omitempty"`   // channel names, pseudonyms, alternate spellings
	Notes    string   `json:"notes,omitempty"`
}

// Company is a legal entity that receives funding as a beneficiary.
// An individual (auto-entrepreneur) also appears as a Company when they
// are the declared beneficiary on the CNC page.
type Company struct {
	ID        string `json:"id"`                    // slug, e.g. "eigengrau-production"
	Name      string `json:"name"`                  // as declared on CNC page
	LegalForm string `json:"legal_form,omitempty"`  // SARL, SAS, SASU, EI, association, etc.
	Notes     string `json:"notes,omitempty"`
}

// Project is a content project submitted for funding. The same project can be
// submitted to multiple commissions (e.g. rejected then resubmitted, or pilot
// then full creation grant).
type Project struct {
	ID     string   `json:"id"`               // slug, e.g. "balade-mentale-bouts-du-monde"
	Title  string   `json:"title"`            // as listed on CNC page
	Format string   `json:"format,omitempty"` // "série", "unitaire"
	Genres []string `json:"genres,omitempty"` // e.g. ["documentaire", "animation"]
}

// Commission is a single jury session for a specific fund.
type Commission struct {
	ID        string         `json:"id"`         // e.g. "cnc-talent-2025-11-12"
	FundName  string         `json:"fund_name"`  // "CNC Talent"
	Date      string         `json:"date"`       // ISO 8601: "2025-11-12"
	SourceURL string         `json:"source_url"` // canonical CNC page URL
	Jury      []JuryPresence `json:"jury"`
	Grants    []Grant        `json:"grants"`
}

// ---------------------------------------------------------------------------
// Edges / relationships
// ---------------------------------------------------------------------------

// JuryPresence is the edge between a Person and a Commission.
// Storing the raw name allows tracing back to the source if the person
// resolution is later updated.
type JuryPresence struct {
	PersonID string `json:"person_id"`        // resolved Person.ID (empty if not yet resolved)
	RawName  string `json:"raw_name"`          // exactly as written on the CNC page
	Role     JuryRole `json:"role"`
}

// JuryRole is the role of a jury member in a commission.
type JuryRole string

const (
	RolePresident          JuryRole = "président"
	RolePresidentSuppleant JuryRole = "président suppléant"
	RoleMembre             JuryRole = "membre"
)

// Grant is the edge between a Project and a Commission — the funding decision.
// It also records the talent identity and beneficiary as raw strings (as found
// on the CNC page) alongside resolved IDs, so the graph can be built
// incrementally as persons/companies are identified.
type Grant struct {
	ID           string `json:"id"` // e.g. "cnc-talent-2025-11-12-balade-mentale"
	ProjectID    string `json:"project_id"`
	CommissionID string `json:"commission_id"`

	// Talent: the creator identity listed on the CNC page.
	// May be a real name, a pseudonym, a channel name, or a team name.
	TalentRaw      string `json:"talent_raw"`                 // e.g. "Balade mentale"
	TalentPersonID string `json:"talent_person_id,omitempty"` // resolved Person.ID

	// Beneficiary: the legal entity that receives the money.
	BeneficiaryRaw       string `json:"beneficiary_raw"`                  // e.g. "Eigengrau production"
	BeneficiaryCompanyID string `json:"beneficiary_company_id,omitempty"` // resolved Company.ID

	Amount     int        `json:"amount"`      // in euros; 0 if rejected
	AidSection AidSection `json:"aid_section"` // top-level section on the page
	AidType    AidType    `json:"aid_type"`    // grant sub-type
	Result     Result     `json:"result"`      // accepted / rejected
}

// AidSection is the top-level section of the commission page.
type AidSection string

const (
	SectionCreation AidSection = "aide_creation"
	SectionChaine   AidSection = "aide_chaine"
)

// AidType is the specific sub-type of grant within a section.
type AidType string

const (
	AidStandard             AidType = "standard"              // standard creation grant
	AidBourseEncouragement  AidType = "bourse_encouragement"  // 2 000 €
	AidPilote               AidType = "aide_pilote"           // 5 000 €
	AidDeveloppementChaine  AidType = "developpement_chaine"  // channel development
)

// Result is the outcome of a grant application.
type Result string

const (
	ResultAccepted Result = "accepted"
	ResultRejected Result = "rejected"
)

// Relationship is a known connection between two persons, to be populated
// via external research (Google, LinkedIn, IMDB, etc.).
// Direction is not meaningful: (A,B) and (B,A) are the same edge — store
// once with IDs sorted alphabetically.
type Relationship struct {
	PersonAID  string           `json:"person_a_id"` // alphabetically first
	PersonBID  string           `json:"person_b_id"` // alphabetically second
	Type       RelationshipType `json:"type"`
	Source     string           `json:"source"`               // URL or description of evidence
	Confidence Confidence       `json:"confidence"`
	Notes      string           `json:"notes,omitempty"`
}

// RelationshipType describes the nature of a connection.
type RelationshipType string

const (
	RelColleague        RelationshipType = "colleague"         // worked at the same company
	RelWorkedTogether   RelationshipType = "worked_together"   // collaborated on a project
	RelFriend           RelationshipType = "friend"            // personal relationship
	RelMentor           RelationshipType = "mentor"            // A mentored B
	RelPubliclyConnected RelationshipType = "publicly_connected" // appear together publicly
)

// Confidence is how certain we are about a relationship.
type Confidence string

const (
	ConfidenceHigh   Confidence = "high"   // direct evidence (joint credit, same company, etc.)
	ConfidenceMedium Confidence = "medium" // indirect but strong signal
	ConfidenceLow    Confidence = "low"    // circumstantial
)
