package app

import "net/http"

func (a *App) adminOperatorDelete(w http.ResponseWriter, r *http.Request, id identity) {
	if !id.User.GlobalAdmin {
		problem(w, 403, "forbidden")
		return
	}
	var input struct {
		Revision int64 `json:"revision"`
	}
	if !body(w, r, &input) {
		return
	}
	tx, current, err := a.accessTransaction(r, id)
	if err != nil {
		dbFailure(w)
		return
	}
	defer tx.Rollback(r.Context())
	if !current.User.GlobalAdmin {
		problem(w, 403, "forbidden")
		return
	}
	operator := r.PathValue("operator")
	if operator == current.User.ID {
		problem(w, 422, "cannot_delete_self")
		return
	}
	// Keep the identity for references in exports, mosaics and audit history.
	result, err := tx.Exec(r.Context(), "UPDATE operators SET deleted_at=now(),active=false,revision=revision+1 WHERE id=$1 AND revision=$2 AND deleted_at IS NULL", operator, input.Revision)
	if err != nil {
		dbFailure(w)
		return
	}
	if result.RowsAffected() != 1 {
		problem(w, 412, "revision_conflict")
		return
	}
	for _, query := range []string{
		"DELETE FROM sessions WHERE operator_id=$1",
		"DELETE FROM group_members WHERE operator_id=$1",
		"DELETE FROM installation_group_members WHERE operator_id=$1",
		"DELETE FROM memberships WHERE operator_id=$1",
	} {
		if _, err = tx.Exec(r.Context(), query, operator); err != nil {
			dbFailure(w)
			return
		}
	}
	if !requireInstallationAdmin(r.Context(), tx, w) {
		return
	}
	if err = audit(r.Context(), tx, current, "", "operator.deleted", operator, input); err != nil {
		dbFailure(w)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		dbFailure(w)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
