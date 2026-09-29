package dto

import "encoding/json"

// The applications review screen's cases (TKT-01M29CP2MG), as a moderator
// sees them; an admin sees the same. Registered into the shared corpus, as
// admin_fixtures_test.go's are.

func init() {
	corpus["admin-applications."+tModerator] = adminApplicationList()
	corpus["admin-application-pending."+tModerator] = adminApplicationPending()
	corpus["admin-application-denied."+tModerator] = adminApplicationDenied()
	corpus["admin-application-approved."+tModerator] = adminApplicationApproved()
	corpus["admin-application-withdrawn."+tModerator] = adminApplicationWithdrawn()
	corpus["application-note."+tModerator] = noteByBob
}

var (
	carolAnswers = json.RawMessage(`{"email":"carol@example.com","name":"Carol Applicant","rules":true,"who":"Alice invited me"}`)

	noteByBob = ApplicationNote{ID: 9, Author: bob, Body: "Alice vouched for her in the book club.",
		CreatedAt: 1757605000000}

	carolPending = AdminApplicationRow{ID: 42, Status: "pending", CreatedAt: 1757600000000, Provider: ptr("github"),
		Email: ptr("carol@example.com"), EmailVerified: true}
	carolDenied = AdminApplicationRow{ID: 17, Status: "denied", CreatedAt: 1757400000000, Provider: ptr("github"),
		Email: ptr("carol@example.com"), EmailVerified: true}
	// goneRow is an application whose identity was removed.
	goneRow = AdminApplicationRow{ID: 40, Status: "pending", CreatedAt: 1757590000000}
)

// adminApplicationList is a page of pending applications, one whose
// identity is gone, with more to come.
func adminApplicationList() AdminApplicationList {
	return AdminApplicationList{Applications: []AdminApplicationRow{carolPending, goneRow}, NextCursor: "MTc1NzU5MDAwMDAwMDo0MA"}
}

// adminApplicationPending is one pending application: the answers, the
// identity, a prior denial, a note from another moderator, and the handle
// approval suggests.
func adminApplicationPending() AdminApplication {
	return AdminApplication{ID: 42, Status: "pending", CreatedAt: 1757600000000, Form: registrationForm(),
		Answers:  carolAnswers,
		Identity: &ApplicantIdentity{Provider: "github", Email: ptr("carol@example.com"), EmailVerified: true},
		Prior:    []AdminApplicationRow{carolDenied}, Notes: []ApplicationNote{noteByBob}, SuggestedHandle: "carol"}
}

// adminApplicationDenied is a denial with a block, from an identity whose
// provider asserted no email.
func adminApplicationDenied() AdminApplication {
	return AdminApplication{ID: 17, Status: "denied", CreatedAt: 1757400000000, Form: registrationForm(),
		Answers:  carolAnswers,
		Identity: &ApplicantIdentity{Provider: "club", Blocked: true},
		Prior:    []AdminApplicationRow{}, Notes: []ApplicationNote{},
		Decision: &ApplicationDecision{By: &bob, At: 1757410000000, Note: "Nobody here knows them."}}
}

// adminApplicationApproved is an approval, with the member it created,
// after the identity was removed.
func adminApplicationApproved() AdminApplication {
	return AdminApplication{ID: 43, Status: "approved", CreatedAt: 1757600000000, Form: registrationForm(),
		Answers: carolAnswers, Prior: []AdminApplicationRow{}, Notes: []ApplicationNote{},
		Decision: &ApplicationDecision{By: &bob, At: 1757610000000, Note: "Welcome.",
			User: &Author{Handle: "carol", DisplayName: "Carol Applicant", Role: "member", Kind: "human"}}}
}

// adminApplicationWithdrawn is the applicant's own withdrawal: a decision
// with no moderator and no note.
func adminApplicationWithdrawn() AdminApplication {
	return AdminApplication{ID: 44, Status: "withdrawn", CreatedAt: 1757600000000, Form: registrationForm(),
		Answers:  carolAnswers,
		Identity: &ApplicantIdentity{Provider: "github", Email: ptr("carol@example.com"), EmailVerified: true},
		Prior:    []AdminApplicationRow{}, Notes: []ApplicationNote{}, SuggestedHandle: "carol",
		Decision: &ApplicationDecision{At: 1757603600000}}
}
