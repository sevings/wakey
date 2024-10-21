package wakey_test

import (
	"testing"
	"time"

	"wakey/internal/wakey"

	"github.com/stretchr/testify/require"
)

func setupTestDB(t *testing.T) *wakey.DB {
	db, ok := wakey.LoadDatabase(":memory:")
	require.True(t, ok, "Failed to load database")
	return db
}

func TestUserOperations(t *testing.T) {
	db := setupTestDB(t)

	user := &wakey.User{
		ID:   1,
		Name: "Test User",
		Bio:  "Test Bio",
		Tz:   0,
	}

	err := db.SaveUser(user)
	require.NoError(t, err)

	fetchedUser, err := db.GetUser(1)
	require.NoError(t, err)
	require.Equal(t, user.Name, fetchedUser.Name)
	require.Equal(t, user.Bio, fetchedUser.Bio)

	_, err = db.GetUser(999)
	require.Error(t, err)
}

func TestPlanOperations(t *testing.T) {
	db := setupTestDB(t)

	user := &wakey.User{ID: 2, Name: "Plan User"}
	err := db.SaveUser(user)
	require.NoError(t, err)

	plan := &wakey.Plan{
		UserID:  2,
		Content: "Test Plan",
		WakeAt:  time.Now().Add(24 * time.Hour),
	}

	err = db.SavePlan(plan)
	require.NoError(t, err)

	fetchedPlan, err := db.GetLatestPlan(2)
	require.NoError(t, err)
	require.Equal(t, plan.Content, fetchedPlan.Content)

	_, err = db.GetLatestPlan(999)
	require.Error(t, err)
}

func TestCopyPlanForNextDay(t *testing.T) {
	db := setupTestDB(t)

	user := &wakey.User{ID: 3, Name: "Copy Plan User"}
	err := db.SaveUser(user)
	require.NoError(t, err)

	originalPlan := &wakey.Plan{
		UserID:  3,
		Content: "Original Plan",
		WakeAt:  time.Now().Add(24 * time.Hour),
	}
	err = db.SavePlan(originalPlan)
	require.NoError(t, err)

	err = db.CopyPlanForNextDay(3)
	require.NoError(t, err)

	newPlan, err := db.GetLatestPlan(3)
	require.NoError(t, err)
	require.Equal(t, originalPlan.Content, newPlan.Content)
	require.True(t, newPlan.WakeAt.After(originalPlan.WakeAt))
	require.WithinDuration(t, originalPlan.WakeAt.Add(24*time.Hour), newPlan.WakeAt, time.Second)

	err = db.CopyPlanForNextDay(999)
	require.Error(t, err)
}

func TestWishOperations(t *testing.T) {
	db := setupTestDB(t)

	user := &wakey.User{ID: 4, Name: "Wish User"}
	err := db.SaveUser(user)
	require.NoError(t, err)

	plan := &wakey.Plan{
		UserID:  4,
		Content: "Wish Plan",
		WakeAt:  time.Now().Add(24 * time.Hour),
	}
	err = db.SavePlan(plan)
	require.NoError(t, err)

	wish := &wakey.Wish{
		FromID:  5,
		PlanID:  plan.ID,
		Content: "Test Wish",
	}

	err = db.SaveWish(wish)
	require.NoError(t, err)

	fetchedWish, err := db.GetWishByID(wish.ID)
	require.NoError(t, err)
	require.Equal(t, wish.Content, fetchedWish.Content)

	_, err = db.GetWishByID(999)
	require.Error(t, err)

	futureWishes, err := db.GetFutureWishes()
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(futureWishes), 1)
}

func TestFindUserForWish(t *testing.T) {
	db := setupTestDB(t)

	user1 := &wakey.User{ID: 6, Name: "Wish User 1"}
	user2 := &wakey.User{ID: 7, Name: "Wish User 2"}
	err := db.SaveUser(user1)
	require.NoError(t, err)
	err = db.SaveUser(user2)
	require.NoError(t, err)

	// Test finding a plan when none are available
	_, err = db.FindUserForWish(6)
	require.Error(t, err)
	require.Equal(t, wakey.ErrNotFound, err)

	plan1 := &wakey.Plan{
		UserID:  6,
		Content: "Plan 1",
		WakeAt:  time.Now().Add(24 * time.Hour),
	}
	plan2 := &wakey.Plan{
		UserID:  7,
		Content: "Plan 2",
		WakeAt:  time.Now().Add(48 * time.Hour),
	}
	err = db.SavePlan(plan1)
	require.NoError(t, err)
	err = db.SavePlan(plan2)
	require.NoError(t, err)

	// Test finding a plan for a wish
	foundPlan, err := db.FindUserForWish(6)
	require.NoError(t, err)
	require.NotNil(t, foundPlan)
	require.Equal(t, int64(7), foundPlan.UserID)

	// Test with non-existent sender ID (should still find a plan)
	nonExistentSenderID := int64(999)
	foundPlan, err = db.FindUserForWish(nonExistentSenderID)
	require.NoError(t, err)
	require.NotNil(t, foundPlan)

	// Add wishes to all plans
	wish1 := &wakey.Wish{
		FromID:  6,
		PlanID:  plan2.ID,
		Content: "Wish for Plan 2",
	}
	err = db.SaveWish(wish1)
	require.NoError(t, err)

	wish2 := &wakey.Wish{
		FromID:  7,
		PlanID:  plan1.ID,
		Content: "Wish for Plan 1",
	}
	err = db.SaveWish(wish2)
	require.NoError(t, err)

	// Now try to find a plan when all have wishes (should return error)
	_, err = db.FindUserForWish(6)
	require.Error(t, err)
	require.Equal(t, wakey.ErrNotFound, err)
}
