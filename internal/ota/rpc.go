package ota

import (
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/Masterminds/semver/v3"
)

// to make the field names consistent with the RPCState struct
var componentFieldMap = map[string]string{
	"app":    "App",
	"system": "System",
}

// RPCState represents the current OTA state for the RPC API
type RPCState struct {
	Updating                   bool       `json:"updating"`
	Error                      string     `json:"error,omitempty"`
	MetadataFetchedAt          *time.Time `json:"metadataFetchedAt,omitempty"`
	AppUpdatePending           bool       `json:"appUpdatePending"`
	SystemUpdatePending        bool       `json:"systemUpdatePending"`
	AppDownloadProgress        *float32   `json:"appDownloadProgress,omitempty"` //TODO: implement for progress bar
	AppDownloadFinishedAt      *time.Time `json:"appDownloadFinishedAt,omitempty"`
	SystemDownloadProgress     *float32   `json:"systemDownloadProgress,omitempty"` //TODO: implement for progress bar
	SystemDownloadFinishedAt   *time.Time `json:"systemDownloadFinishedAt,omitempty"`
	AppVerificationProgress    *float32   `json:"appVerificationProgress,omitempty"`
	AppVerifiedAt              *time.Time `json:"appVerifiedAt,omitempty"`
	SystemVerificationProgress *float32   `json:"systemVerificationProgress,omitempty"`
	SystemVerifiedAt           *time.Time `json:"systemVerifiedAt,omitempty"`
	AppUpdateProgress          *float32   `json:"appUpdateProgress,omitempty"` //TODO: implement for progress bar
	AppUpdatedAt               *time.Time `json:"appUpdatedAt,omitempty"`
	SystemUpdateProgress       *float32   `json:"systemUpdateProgress,omitempty"` //TODO: port rk_ota, then implement
	SystemUpdatedAt            *time.Time `json:"systemUpdatedAt,omitempty"`
}

func setTimeIfNotZero(rpcVal reflect.Value, i int, status time.Time) {
	if !status.IsZero() {
		rpcVal.Field(i).Set(reflect.ValueOf(&status))
	}
}

func setFloat32IfNotZero(rpcVal reflect.Value, i int, status float32) {
	if status != 0 {
		rpcVal.Field(i).Set(reflect.ValueOf(&status))
	}
}

// applyComponentStatusToRPCState uses reflection to map componentUpdateStatus fields to RPCState
func applyComponentStatusToRPCState(component string, status componentUpdateStatus, rpcState *RPCState) {
	prefix := componentFieldMap[component]
	if prefix == "" {
		return
	}

	rpcVal := reflect.ValueOf(rpcState).Elem()

	// it's really inefficient, but hey we do not need to use this often
	// componentUpdateStatus is for internal use only, and all fields are unexported
	for i := 0; i < rpcVal.NumField(); i++ {
		rpcFieldName, hasPrefix := strings.CutPrefix(rpcVal.Type().Field(i).Name, prefix)
		if !hasPrefix {
			continue
		}

		switch rpcFieldName {
		case "DownloadProgress":
			setFloat32IfNotZero(rpcVal, i, status.downloadProgress)
		case "DownloadFinishedAt":
			setTimeIfNotZero(rpcVal, i, status.downloadFinishedAt)
		case "VerificationProgress":
			setFloat32IfNotZero(rpcVal, i, status.verificationProgress)
		case "VerifiedAt":
			setTimeIfNotZero(rpcVal, i, status.verifiedAt)
		case "UpdateProgress":
			setFloat32IfNotZero(rpcVal, i, status.updateProgress)
		case "UpdatedAt":
			setTimeIfNotZero(rpcVal, i, status.updatedAt)
		case "UpdatePending":
			rpcVal.Field(i).SetBool(status.pending)
		default:
			continue
		}
	}
}

// ToRPCState converts the State to the RPCState
func (s *State) ToRPCState() *RPCState {
	r := &RPCState{
		Updating:          s.updating,
		Error:             s.error,
		MetadataFetchedAt: &s.metadataFetchedAt,
	}

	for component, status := range s.componentUpdateStatuses {
		applyComponentStatusToRPCState(component, status, r)
	}

	return r
}

func remoteMetadataToComponentStatus(
	remoteMetadata *UpdateMetadata,
	component string,
	componentStatus *componentUpdateStatus,
	params UpdateParams,
) error {
	prefix := componentFieldMap[component]
	if prefix == "" {
		return fmt.Errorf("unknown component: %s", component)
	}

	remoteMetadataVal := reflect.ValueOf(remoteMetadata).Elem()
	for i := 0; i < remoteMetadataVal.NumField(); i++ {
		fieldName, hasPrefix := strings.CutPrefix(remoteMetadataVal.Type().Field(i).Name, prefix)
		if !hasPrefix {
			continue
		}

		switch fieldName {
		case "URL":
			componentStatus.url = remoteMetadataVal.Field(i).String()
		case "Hash":
			componentStatus.hash = remoteMetadataVal.Field(i).String()
		case "Version":
			componentStatus.version = remoteMetadataVal.Field(i).String()
		case "SigURL":
			componentStatus.sigUrl = remoteMetadataVal.Field(i).String()
		default:
			// fmt.Printf("unknown field %s", fieldName)
			continue
		}
	}

	localVersion, err := semver.NewVersion(componentStatus.localVersion)
	if err != nil {
		return fmt.Errorf("error parsing local version: %w", err)
	}

	remoteVersion, err := semver.NewVersion(componentStatus.version)
	if err != nil {
		return fmt.Errorf("error parsing remote version: %w", err)
	}
	componentStatus.available = remoteVersion.GreaterThan(localVersion)
	componentStatus.availableReason = fmt.Sprintf("remote version %s is greater than local version %s", remoteVersion.String(), localVersion.String())

	// Handle pre-release updates
	if remoteVersion.Prerelease() != "" && params.IncludePreRelease && componentStatus.available {
		componentStatus.availableReason += " (pre-release)"
	}

	// If a custom version is specified, use it to determine if the update is available
	constraint, componentExists := params.Components[component]
	// we don't need to check again if it's already available
	if componentExists && constraint != "" {
		componentStatus.available = componentStatus.version != componentStatus.localVersion
		if componentStatus.available {
			componentStatus.availableReason = fmt.Sprintf("custom version %s is not equal to local version %s", constraint, componentStatus.localVersion)
			componentStatus.customVersionUpdate = true
		}
	} else if !componentExists {
		componentStatus.available = false
		componentStatus.availableReason = "component not specified in update parameters"
	}

	return nil
}
