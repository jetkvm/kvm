package usbgadget

// USB endpoint budget for the JetKVM RV1106 dwc3 controller.
//
// dwc3 exposes IN (device->host) and OUT (host->device) endpoints as separate
// hardware resources, so they are budgeted independently — allocating a
// bulk-OUT never steals from the IN pool.
//
// The IN budget of 7 is empirically confirmed: the full default function set
// plus CDC-NCM needs 8 IN endpoints and NCM's bulk-IN silently fails to
// allocate (the link enumerates and usb0 comes up RX-only, TX is a black
// hole); dropping any single IN-using function (-> 7) makes it work. The OUT
// pool on this part is symmetric with IN and OUT demand stays far below it in
// practice, so the same ceiling is used for both.
//
// Note: the true low-level limiter for IN endpoints may be dwc3's TX-FIFO
// SRAM (each IN endpoint reserves FIFO space sized to its max packet) rather
// than a raw endpoint count, but the effective ceiling we observed is 7, so a
// simple count captures it. FIFO RAM is not modeled separately.
const (
	usbInEndpointBudget  uint = 7
	usbOutEndpointBudget uint = 7
)

// endpointCost is the number of USB IN and OUT endpoints a gadget function
// consumes.
type endpointCost struct {
	in  uint
	out uint
}

// endpointCosts maps a gadget config item key to its endpoint cost. Keys match
// entries in defaultGadgetConfig; items absent here cost nothing (base,
// base_info, mass_storage_lun0, ...). HID OUT cost follows the function's
// no_out_endpoint attribute (keyboard has an interrupt-OUT for LED reports;
// the mice and wake HID do not).
var endpointCosts = map[string]endpointCost{
	"keyboard":          {in: 1, out: 1}, // interrupt IN + interrupt OUT (LED reports)
	"wake_hid":          {in: 1, out: 0}, // interrupt IN only
	"absolute_mouse":    {in: 1, out: 0}, // interrupt IN only
	"relative_mouse":    {in: 1, out: 0}, // interrupt IN only
	"audio":             {in: 1, out: 0}, // UAC1 capture: isochronous IN
	"mass_storage_base": {in: 1, out: 1}, // bulk IN + bulk OUT
	"serial_console":    {in: 2, out: 1}, // CDC-ACM: bulk IN + notify IN + bulk OUT
	"ncm":               {in: 2, out: 1}, // CDC-NCM: bulk IN + notify IN + bulk OUT
}

// endpointUsage returns the IN and OUT endpoint demand of a device selection,
// including always-on functions (wake_hid) as well as toggleable ones.
func endpointUsage(devices *Devices) (in, out uint) {
	for key, cost := range endpointCosts {
		if isGadgetConfigItemEnabledForDevices(key, devices) {
			in += cost.in
			out += cost.out
		}
	}
	return in, out
}

// ExceedsEndpointBudget reports whether the given device selection would exceed
// the controller's IN or OUT endpoint budget. An over-budget gadget still
// enumerates but leaves some function's endpoint(s) silently unallocated (e.g.
// CDC-NCM comes up RX-only with a dead TX path), so the UI uses this to warn
// before the user commits to the combination.
func ExceedsEndpointBudget(devices *Devices) bool {
	in, out := endpointUsage(devices)
	return in > usbInEndpointBudget || out > usbOutEndpointBudget
}
