// Package sysproxy reads and sets the system proxy: the setting apps that
// honour it send their traffic through. Whether Hop may change it is
// service.SystemProxyService's decision; this package only does what it is
// told.
package sysproxy

// Controller reads and sets the system proxy of this machine.
type Controller struct{}

// New returns a controller of this machine's system proxy.
func New() *Controller {
	return &Controller{}
}
