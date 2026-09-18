package model

// HasUserPassword deliberately reads the database, independently of the user
// cache and GetUserById's password omission. This also covers existing sessions.
func HasUserPassword(userID int) (bool, error) {
	var state struct {
		Password string
	}
	err := DB.Model(&User{}).Select("password").Where("id = ?", userID).First(&state).Error
	return state.Password != "", err
}

// UpdateUserPasswordIfUnchanged prevents two first-time password requests from
// overwriting each other, and rejects changes based on an obsolete password.
func UpdateUserPasswordIfUnchanged(userID int, previousHash, nextHash string) (bool, error) {
	result := DB.Model(&User{}).Where("id = ? AND password = ?", userID, previousHash).Update("password", nextHash)
	return result.RowsAffected == 1, result.Error
}
