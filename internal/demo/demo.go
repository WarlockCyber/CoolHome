// Package demo provides an in-memory fake home so the app can run and be
// exercised without Tuya credentials.
package demo

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/WarlockCyber/CoolHome/internal/tuya"
)

// Provider implements home.Provider with a handful of fake devices.
type Provider struct {
	mu      sync.Mutex
	order   []string
	devices map[string]*tuya.Device
	specs   map[string]tuya.Spec
	started time.Time
	now     func() time.Time
}

// New builds the demo home.
func New() *Provider {
	p := &Provider{
		devices: map[string]*tuya.Device{},
		specs:   map[string]tuya.Spec{},
		started: time.Now(),
		now:     time.Now,
	}
	p.add(tuya.Device{
		ID: "demo-light-1", Name: "Лампа в гостиной", Category: "dj",
		ProductName: "Умная лампа", Online: true,
		Status: []tuya.DeviceStatus{
			{Code: "switch_led", Value: true},
			{Code: "bright_value", Value: 640},
			{Code: "work_mode", Value: "white"},
		},
	}, tuya.Spec{
		Category: "dj",
		Functions: []tuya.Function{
			{Code: "switch_led", Type: "Boolean", Values: raw(`{}`), Name: "Свет"},
			{Code: "bright_value", Type: "Integer", Values: raw(`{"min":10,"max":1000,"step":1,"scale":0}`), Name: "Яркость"},
			{Code: "work_mode", Type: "Enum", Values: raw(`{"range":["white","colour"]}`), Name: "Режим"},
		},
		Status: []tuya.Function{
			{Code: "switch_led", Type: "Boolean", Name: "Свет"},
			{Code: "bright_value", Type: "Integer", Values: raw(`{"scale":0}`), Name: "Яркость"},
			{Code: "work_mode", Type: "Enum", Values: raw(`{"range":["white","colour"]}`), Name: "Режим"},
		},
	})

	p.add(tuya.Device{
		ID: "demo-plug-1", Name: "Кофемашина", Category: "cz",
		ProductName: "Умная розетка", Online: true,
		Status: []tuya.DeviceStatus{{Code: "switch_1", Value: false}},
	}, tuya.Spec{
		Category:  "cz",
		Functions: []tuya.Function{{Code: "switch_1", Type: "Boolean", Values: raw(`{}`), Name: "Питание"}},
		Status:    []tuya.Function{{Code: "switch_1", Type: "Boolean", Name: "Питание"}},
	})

	p.add(tuya.Device{
		ID: "demo-thermo-1", Name: "Термостат на кухне", Category: "wk",
		ProductName: "Термостат", Online: true,
		Status: []tuya.DeviceStatus{
			{Code: "switch", Value: true},
			{Code: "temp_set", Value: 220},
			{Code: "temp_current", Value: 214},
		},
	}, tuya.Spec{
		Category: "wk",
		Functions: []tuya.Function{
			{Code: "switch", Type: "Boolean", Values: raw(`{}`), Name: "Питание"},
			{Code: "temp_set", Type: "Integer", Values: raw(`{"min":50,"max":300,"step":5,"scale":1,"unit":"°C"}`), Name: "Целевая температура"},
		},
		Status: []tuya.Function{
			{Code: "switch", Type: "Boolean", Name: "Питание"},
			{Code: "temp_set", Type: "Integer", Values: raw(`{"scale":1,"unit":"°C"}`), Name: "Целевая температура"},
			{Code: "temp_current", Type: "Integer", Values: raw(`{"scale":1,"unit":"°C"}`), Name: "Текущая температура"},
		},
	})

	p.add(tuya.Device{
		ID: "demo-sensor-1", Name: "Датчик климата в спальне", Category: "wsdcg",
		ProductName: "Датчик температуры и влажности", Online: true,
		Status: []tuya.DeviceStatus{
			{Code: "va_temperature", Value: 231},
			{Code: "va_humidity", Value: 41},
			{Code: "battery_percentage", Value: 90},
		},
	}, tuya.Spec{
		Category: "wsdcg",
		Status: []tuya.Function{
			{Code: "va_temperature", Type: "Integer", Values: raw(`{"scale":1,"unit":"°C"}`), Name: "Температура"},
			{Code: "va_humidity", Type: "Integer", Values: raw(`{"scale":0,"unit":"%"}`), Name: "Влажность"},
			{Code: "battery_percentage", Type: "Integer", Values: raw(`{"scale":0,"unit":"%"}`), Name: "Батарея"},
		},
	})

	p.add(tuya.Device{
		ID: "demo-curtain-1", Name: "Шторы в кабинете", Category: "cl",
		ProductName: "Мотор штор", Online: true,
		Status: []tuya.DeviceStatus{
			{Code: "control", Value: "stop"},
			{Code: "percent_control", Value: 40},
		},
	}, tuya.Spec{
		Category: "cl",
		Functions: []tuya.Function{
			{Code: "control", Type: "Enum", Values: raw(`{"range":["open","stop","close"]}`), Name: "Управление"},
			{Code: "percent_control", Type: "Integer", Values: raw(`{"min":0,"max":100,"step":1,"scale":0,"unit":"%"}`), Name: "Открытие"},
		},
		Status: []tuya.Function{
			{Code: "control", Type: "Enum", Values: raw(`{"range":["open","stop","close"]}`), Name: "Управление"},
			{Code: "percent_control", Type: "Integer", Values: raw(`{"scale":0,"unit":"%"}`), Name: "Открытие"},
		},
	})

	p.add(tuya.Device{
		ID: "demo-humidifier-1", Name: "Увлажнитель в детской", Category: "jsq",
		ProductName: "Увлажнитель", Online: false,
		Status: []tuya.DeviceStatus{
			{Code: "switch_1", Value: false},
			{Code: "humidity", Value: 35},
		},
	}, tuya.Spec{
		Category:  "jsq",
		Functions: []tuya.Function{{Code: "switch_1", Type: "Boolean", Values: raw(`{}`), Name: "Питание"}},
		Status: []tuya.Function{
			{Code: "switch_1", Type: "Boolean", Name: "Питание"},
			{Code: "humidity", Type: "Integer", Values: raw(`{"scale":0,"unit":"%"}`), Name: "Влажность"},
		},
	})
	return p
}

func raw(s string) json.RawMessage { return json.RawMessage(s) }

func (p *Provider) add(d tuya.Device, s tuya.Spec) {
	clone := d
	clone.Status = append([]tuya.DeviceStatus(nil), d.Status...)
	p.devices[d.ID] = &clone
	p.specs[d.ID] = s
	p.order = append(p.order, d.ID)
}

// ListDevices implements home.Provider.
func (p *Provider) ListDevices(_ context.Context, ids []string) ([]tuya.Device, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.tickLocked()

	if len(ids) > 0 {
		out := make([]tuya.Device, 0, len(ids))
		for _, id := range ids {
			if d, ok := p.devices[id]; ok {
				out = append(out, cloneDevice(*d))
			}
		}
		return out, nil
	}
	out := make([]tuya.Device, 0, len(p.order))
	for _, id := range p.order {
		out = append(out, cloneDevice(*p.devices[id]))
	}
	return out, nil
}

// Device implements home.Provider.
func (p *Provider) Device(_ context.Context, id string) (tuya.Device, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.tickLocked()
	d, ok := p.devices[id]
	if !ok {
		return tuya.Device{}, fmt.Errorf("demo: unknown device %q", id)
	}
	return cloneDevice(*d), nil
}

// Spec implements home.Provider.
func (p *Provider) Spec(_ context.Context, id string) (tuya.Spec, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	s, ok := p.specs[id]
	if !ok {
		return tuya.Spec{}, fmt.Errorf("demo: unknown device %q", id)
	}
	return s, nil
}

// Command implements home.Provider.
func (p *Provider) Command(_ context.Context, id string, cmds []tuya.Command) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	d, ok := p.devices[id]
	if !ok {
		return fmt.Errorf("demo: unknown device %q", id)
	}
	if !d.Online {
		return fmt.Errorf("demo: device %q is offline", id)
	}
	for _, cmd := range cmds {
		setStatus(d, cmd.Code, cmd.Value)
	}
	return nil
}

func setStatus(d *tuya.Device, code string, value any) {
	for i := range d.Status {
		if d.Status[i].Code == code {
			d.Status[i].Value = normalize(code, value)
			return
		}
	}
	d.Status = append(d.Status, tuya.DeviceStatus{Code: code, Value: normalize(code, value)})
}

// normalize keeps demo values in the same JSON shape the real cloud reports:
// integers stay integers even though the HTTP layer decoded them as json.Number.
func normalize(code string, value any) any {
	switch code {
	case "bright_value", "temp_set", "percent_control", "humidity":
		if n, ok := toInt(value); ok {
			return n
		}
	}
	return value
}

func toInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	case json.Number:
		i, err := n.Int64()
		return int(i), err == nil
	}
	return 0, false
}

func cloneDevice(d tuya.Device) tuya.Device {
	d.Status = append([]tuya.DeviceStatus(nil), d.Status...)
	return d
}

// tickLocked makes the climate sensor move a little so the UI shows live data.
func (p *Provider) tickLocked() {
	sensor, ok := p.devices["demo-sensor-1"]
	if !ok {
		return
	}
	t := p.now().Sub(p.started).Seconds()
	temp := 231 + int(math.Round(12*math.Sin(t/90)))
	humidity := 41 + int(math.Round(4*math.Sin(t/60)))
	setStatus(sensor, "va_temperature", temp)
	setStatus(sensor, "va_humidity", humidity)
}
