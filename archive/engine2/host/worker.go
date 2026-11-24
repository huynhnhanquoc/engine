package host

import "github.com/kitwork/engine2/core/runtime"

// worker is one VM of a site with the deadline its runs read, kept together between requests
// (Site.machines), so that a request allocates neither.
type worker struct {
	machine  *runtime.VM
	deadline deadline
}
