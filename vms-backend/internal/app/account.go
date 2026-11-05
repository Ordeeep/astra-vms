package app

import (
	"errors"
	"net/http"
	"net/mail"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

// updateProfile always targets the authenticated operator, independently of
// tenant permissions. Email changes require the current password because email
// is also a login identifier. Identifier writes share accessTransaction's lock.
func (a *App) updateProfile(w http.ResponseWriter, r *http.Request, id identity) {
	var input struct {
		Name            string `json:"name"`
		Email           string `json:"email"`
		Revision        int64  `json:"revision"`
		CurrentPassword string `json:"currentPassword"`
	}
	if !body(w, r, &input) {
		return
	}
	input.Name = strings.TrimSpace(input.Name)
	input.Email = strings.TrimSpace(input.Email)
	if input.Name == "" || utf8.RuneCountInString(input.Name) > 128 || strings.ContainsFunc(input.Name, unicode.IsControl) {
		problem(w, 422, "invalid_profile_name")
		return
	}
	address, err := mail.ParseAddress(input.Email)
	if err != nil || address.Address != input.Email || len(input.Email) > 254 || !strings.Contains(input.Email, "@") {
		problem(w, 422, "invalid_email")
		return
	}
	tx, current, err := a.accessTransaction(r, id)
	if err != nil {
		dbFailure(w)
		return
	}
	defer tx.Rollback(r.Context())
	if current.User.Revision != input.Revision {
		problem(w, 412, "revision_conflict")
		return
	}
	var hash, storedEmail string
	if err = tx.QueryRow(r.Context(), "SELECT password_hash,email FROM operators WHERE id=$1 FOR UPDATE", current.User.ID).Scan(&hash, &storedEmail); err != nil {
		dbFailure(w)
		return
	}
	if !strings.EqualFold(storedEmail, input.Email) {
		if len(input.CurrentPassword) == 0 || len(input.CurrentPassword) > 1024 {
			problem(w, 403, "current_password_required")
			return
		}
		select {
		case a.hashSlots <- struct{}{}:
			defer func() { <-a.hashSlots }()
		default:
			problem(w, 503, "authentication_busy")
			return
		}
		if !passwordMatches(hash, input.CurrentPassword) {
			problem(w, 403, "current_password_incorrect")
			return
		}
	}
	var identifierUsed bool
	if err = tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM operators WHERE id<>$1
		AND (lower(email)=lower($2) OR lower(username)=lower($2)))`, current.User.ID, input.Email).Scan(&identifierUsed); err != nil {
		dbFailure(w)
		return
	}
	if identifierUsed {
		problem(w, 409, "email_unavailable")
		return
	}
	err = tx.QueryRow(r.Context(), `UPDATE operators SET name=$1,email=$2,revision=revision+1
		WHERE id=$3 AND revision=$4 RETURNING revision`, input.Name, input.Email, current.User.ID, input.Revision).Scan(&current.User.Revision)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			problem(w, 412, "revision_conflict")
		} else {
			dbFailure(w)
		}
		return
	}
	if err = audit(r.Context(), tx, current, "", "operator.profile_changed", current.User.ID, nil); err != nil {
		dbFailure(w)
		return
	}
	if tx.Commit(r.Context()) != nil {
		dbFailure(w)
		return
	}
	current.User.Name, current.User.Email = input.Name, input.Email
	response(w, 200, current.User)
}
