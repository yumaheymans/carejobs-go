package carejobs

// Kind is a kind of healthcare job, the `kind` filter of the API. The slugs
// are the ones the live tools/list answer allows; a test compares them with
// the server so drift is caught.
//
// Results decode a kind into a plain Kind, so a slug the server adds later
// still arrives as a value (it just has no constant here yet).
type Kind string

// The 17 kinds of job.
const (
	KindNurses             Kind = "nurses"              // Registered nurses and LPNs
	KindNursePractitioners Kind = "nurse-practitioners" // NPs and PAs
	KindPhysicians         Kind = "physicians"          // Doctors
	KindNursingAssistants  Kind = "nursing-assistants"  // CNAs and patient care techs
	KindCaregivers         Kind = "caregivers"          // Caregivers and home health aides
	KindTherapyRehab       Kind = "therapy-rehab"       // Physical, occupational, speech and respiratory therapy
	KindMentalHealth       Kind = "mental-health"       // Mental health and social work
	KindPharmacy           Kind = "pharmacy"            // Pharmacists and pharmacy techs
	KindImaging            Kind = "imaging"             // Radiology and imaging techs
	KindLab                Kind = "lab"                 // Lab
	KindSurgicalTechs      Kind = "surgical-techs"      // Surgical techs
	KindEMTsParamedics     Kind = "emts-paramedics"     // EMS
	KindMedicalAssistants  Kind = "medical-assistants"  // Medical assistants
	KindDental             Kind = "dental"              // Dental
	KindAdmin              Kind = "admin"               // Front desk and admin
	KindAlliedHealth       Kind = "allied-health"       // Allied health
	KindAITraining         Kind = "ai-training"         // AI training for health professionals
)

// Kinds lists every Kind constant, in the order the API documents them.
var Kinds = []Kind{
	KindNurses,
	KindNursePractitioners,
	KindPhysicians,
	KindNursingAssistants,
	KindCaregivers,
	KindTherapyRehab,
	KindMentalHealth,
	KindPharmacy,
	KindImaging,
	KindLab,
	KindSurgicalTechs,
	KindEMTsParamedics,
	KindMedicalAssistants,
	KindDental,
	KindAdmin,
	KindAlliedHealth,
	KindAITraining,
}

// Valid reports whether k is one of the Kind constants.
func (k Kind) Valid() bool {
	for _, known := range Kinds {
		if k == known {
			return true
		}
	}
	return false
}

// Sort is the order of SearchJobs results.
type Sort string

// Sort orders. The server's default is SortMixed.
const (
	SortMixed      Sort = "mixed"       // the board's order, employers interleaved
	SortNewest     Sort = "newest"      // most recently listed first
	SortHighestPay Sort = "highest_pay" // highest posted pay first
)

// EmployerType says what an employer list entry is.
type EmployerType string

// The three types of entry in the employer list.
const (
	EmployerTypeEmployer  EmployerType = "employer"   // a hospital, clinic or care company
	EmployerTypeAIProgram EmployerType = "ai-program" // a program paying health professionals for remote AI work
	EmployerTypeJobBoard  EmployerType = "job-board"  // a source of other employers' jobs
)

// Care is a kind of care, the `care` filter of Employers.
type Care string

// The kinds of care an employer list can be filtered by.
const (
	CareHealthSystem      Care = "health-system"
	CareChildrensHospital Care = "childrens-hospital"
	CareOutpatient        Care = "outpatient"
	CareSpecialty         Care = "specialty-care"
	CareBehavioralHealth  Care = "behavioral-health"
	CareAutism            Care = "autism-care"
	CareHomeCare          Care = "home-care"
	CareSeniorCare        Care = "senior-care"
	CareRehab             Care = "rehab"
	CareDental            Care = "dental"
	CarePharmacyRetail    Care = "pharmacy-retail"
	CareKidney            Care = "kidney-care"
	CareLabsImaging       Care = "labs-imaging"
	CareTelehealth        Care = "telehealth"
	CareStaffing          Care = "staffing"
	CareAITraining        Care = "ai-training"
	CareJobBoards         Care = "job-boards"
)
