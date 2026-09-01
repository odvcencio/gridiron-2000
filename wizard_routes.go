package main

import "m31labs.dev/gosx/route"

// registerWizardRoutes mounts every wizard step (design section 4.1,
// steps 1-9 and 13 — this slice's scope) onto the SETUP app's router.
// Called once from buildSetupAppWithTokenSink, alongside the bare /setup
// root and its token-claim action.
func registerWizardRoutes(router *route.Router, rt *SetupRuntime) {
	registerConfigSteps(router, rt)
	registerMembershipStep(router, rt)
	registerCommissionerStep(router, rt)
	registerReviewStep(router, rt)
}
