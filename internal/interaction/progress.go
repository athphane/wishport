package interaction

// Preparation stages describe workflow position, not elapsed-time estimates.
// Authentication can succeed early and skip the installation stages.
const (
	StageAuthentication = "Checking SSH authentication"
	StageTarget         = "Verifying Forge target"
	StageLocalKey       = "Checking local SSH key"
	StageApproval       = "Awaiting key installation approval"
	StageRecheck        = "Rechecking Forge target"
	StageExistingKeys   = "Checking Forge keys"
	StageSubmitting     = "Submitted or pending; installation may still finish"
	StageSubmitted      = "Submitted"
	StageInstallation   = "Waiting for key installation"
	StageRetry          = "Checking installed key"
)

// PreparationStep returns zero for operations with no known progress total.
func PreparationStep(stage string) (step, total int) {
	switch stage {
	case StageAuthentication:
		step = 1
	case StageTarget:
		step = 2
	case StageLocalKey:
		step = 3
	case StageApproval:
		step = 4
	case StageRecheck, StageExistingKeys, StageSubmitting, StageSubmitted:
		step = 5
	case StageInstallation:
		step = 6
	case StageRetry:
		step = 7
	default:
		return 0, 0
	}
	return step, 7
}
