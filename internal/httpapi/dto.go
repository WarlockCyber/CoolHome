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

func newSnapshotDTO(snap home.Snapshot, demo, ir bool) snapshotResponse {
	out := snapshotResponse{
		Devices: make([]deviceDTO, 0, len(snap.Devices)),
		Error:   snap.Error,
		Demo:    demo,
		IR:      ir,
	}
	if !snap.LastSync.IsZero() {
		t := snap.LastSync
		out.LastSync = &t
	}
	for _, v := range snap.Devices {
		out.Devices = append(out.Devices, newDeviceDTO(v))
	}
	return out
}

func newDeviceDTO(v home.DeviceView) deviceDTO {
	dto := deviceDTO{
		ID:        v.ID,
		Name:      v.Name,
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
