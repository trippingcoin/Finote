package repo

import (
	"context"
	"finote/internal/payment/model"
)

func (r *Repo) ListUserProfiles(ctx context.Context) ([]model.User, error) {
	rows, err := r.query.QueryContext(ctx, `SELECT id,display_name,username FROM users WHERE ($1='' OR id=$1) ORDER BY id`, r.owner)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	users := []model.User{}
	for rows.Next() {
		var u model.User
		if err := rows.Scan(&u.ID, &u.DisplayName, &u.Username); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}
