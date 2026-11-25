package httpapi

import (
	"time"

	"github.com/WarlockCyber/CoolHome/internal/home"
	"github.com/WarlockCyber/CoolHome/internal/tuya"
)

type snapshotResponse struct {
	Devices  []deviceDTO `json:"devices"`
	LastSync *time.Time  `json:"last_sync,omitempty"`
	Error    string      `json:"error,omitempty"`
	Demo     bool        `json:"demo"`
	IR       bool        `json:"ir,omitempty"`
	// Build identifies the bundle the server is running. A page that was
	// rendered from a different one reloads itself: the dashboard on a wall
	// stays open for weeks, and a restart must not leave it on old code.
	Build string `json:"build,omitempty"`
}

type deviceDTO struct {
	ID        string      `json:"id"`
	Name      string      `json:"name"`
	Category  string      `json:"category"`
	Product   string      `json:"product,omitempty"`
	Online    bool        `json:"online"`
	Status    []statusDTO `json:"status"`
	Functions []pointDTO  `json:"functions"`
	Readings  []pointDTO  `json:"readings"`
	SpecError string      `json:"spec_error,omitempty"`
	// SpecUnsupported marks a device the cloud does not describe over the
	// data-point API at all, so the UI can explain it instead of showing an
	// error that looks like a fault.
	SpecUnsupported bool `json:"spec_unsupported,omitempty"`
	// Note explains a device that is visible but not controllable.
	Note string `json:"note,omitempty"`
	// CloudName is what the cloud calls the device, present only when a local
	// name overrides it. The page needs it to offer "use the cloud's name
	// again" and to know what the old name was when it renames widgets.
	CloudName string `json:"cloud_name,omitempty"`
	// Renamed says a local name overrides the cloud's. It is a flag rather than
	// an inference from CloudName because a device whose cloud name is empty
	// would otherwise look unrenamed, and saving its dialog without edits would
	// quietly delete the local name.
	Renamed bool `json:"renamed,omitempty"`
}

type statusDTO struct {
	Code  string `json:"code"`
	Value any    `json:"value"`
}

// pointDTO describes a data point for the UI: a writable one (function) or a
// reported one (reading), with its domain already parsed out of the raw JSON.
type pointDTO struct {
	Code    string   `json:"code"`
	Name    string   `json:"name"`
	Type    string   `json:"type"`
	Min     *int     `json:"min,omitempty"`
	Max     *int     `json:"max,omitempty"`
	Step    int      `json:"step,omitempty"`
	Scale   int      `json:"scale,omitempty"`
	Unit    string   `json:"unit,omitempty"`
	Options []string `json:"options,omitempty"`
}

func newSnapshotDTO(snap home.Snapshot, demo, ir bool, build string, names map[string]string) snapshotResponse {
	out := snapshotResponse{
		Devices: make([]deviceDTO, 0, len(snap.Devices)),
		Error:   snap.Error,
		Demo:    demo,
		IR:      ir,
		Build:   build,
	}
	if !snap.LastSync.IsZero() {
		t := snap.LastSync
		out.LastSync = &t
	}
	for _, v := range snap.Devices {
		out.Devices = append(out.Devices, newDeviceDTO(v, names))
	}
	return out
}

// newDeviceDTO maps one device. `names` carries the installation's local names:
// a name set here overrides what the cloud calls the device, and the cloud is
// never told about it.
func newDeviceDTO(v home.DeviceView, names map[string]string) deviceDTO {
	name := v.Name
	local := names[v.ID]
	if local != "" {
		name = local
	}
	dto := deviceDTO{
		ID:        v.ID,
		Name:      name,
		CloudName: cloudNameIfRenamed(v.Name, local),
		Renamed:   local != "",
		Category:  v.Category,
		Product:   v.ProductName,
		Online:    v.Online,
		Status:    make([]statusDTO, 0, len(v.Status)),
		Functions: []pointDTO{},
		Readings:  []pointDTO{},
		SpecError: v.SpecError,

		SpecUnsupported: v.SpecUnsupported,
		Note:            v.Note,
	}
	for _, s := range v.Status {
		dto.Status = append(dto.Status, statusDTO{Code: s.Code, Value: s.Value})
	}
	if v.Spec == nil {
		return dto
	}
	for _, f := range v.Spec.Functions {
		dto.Functions = append(dto.Functions, newPointDTO(f))
	}
	for _, f := range v.Spec.Status {
		dto.Readings = append(dto.Readings, newPointDTO(f))
	}
	return dto
}

// cloudNameIfRenamed reports the cloud's name only when a local one shadows it.
// Sending both names for every device would double the snapshot for nothing.
func cloudNameIfRenamed(cloud, local string) string {
	if local == "" || local == cloud {
		return ""
	}
	return cloud
}

func newPointDTO(f tuya.Function) pointDTO {
	vs := f.ValueSpec()
	name := f.Name
	if name == "" {
		name = f.Code
	}
	return pointDTO{
		Code:    f.Code,
		Name:    name,
		Type:    f.Type,
		Min:     vs.Min,
		Max:     vs.Max,
		Step:    vs.Step,
		Scale:   vs.Scale,
		Unit:    vs.Unit,
		Options: vs.Range,
	}
}
