package supervisor

const (
	EnvChildID        = "JETKVM_CHILD_ID"            // The child ID is the version of the app that is running
	EnvSubcomponent   = "JETKVM_SUBCOMPONENT"        // The subcomponent is the component that is running
	ErrorDumpDir      = "/userdata/jetkvm/crashdump" // The error dump directory is the directory where the error dumps are stored
	ErrorDumpLastFile = "last-crash.log"             // The error dump last file is the last error dump file
	ErrorDumpTemplate = "jetkvm-%s.log"              // The error dump template is the template for the error dump file

	FailsafeReasonVideoMaxRestartAttemptsReached = "failsafe::video.max_restart_attempts_reached"
)
