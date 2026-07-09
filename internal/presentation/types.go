package presentation

import "keydeck.local/feasibilitylab/internal/corehost"

// Identity is the authenticated core identity exposed through the presentation boundary.
type Identity = corehost.Identity

// TaskCreateRequest is the canonical task-create command accepted by the presentation boundary.
type TaskCreateRequest = corehost.TaskCreateRequest

// TaskCreateResult is the canonical task-create result returned by the presentation boundary.
type TaskCreateResult = corehost.TaskCreateResult
