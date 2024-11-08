package wakey_test

import (
	"database/sql"
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

	// Create a user
	user := &wakey.User{
		ID:   1,
		Name: "Test User",
		Bio:  "Test Bio",
		Tz:   0,
	}
	err := db.CreateUser(user)
	require.NoError(t, err)

	// Test attempting to create a user with an existing ID
	duplicateUser := &wakey.User{
		ID:   1,
		Name: "Duplicate User",
		Bio:  "Duplicate Bio",
		Tz:   0,
	}
	err = db.CreateUser(duplicateUser)
	require.Error(t, err)
	require.Contains(t, err.Error(), "already exists")

	// Test updating an existing user
	user.Name = "Updated Name"
	user.Bio = "Updated Bio"
	err = db.SaveUser(user)
	require.NoError(t, err)

	fetchedUser, err := db.GetUserByID(1)
	require.NoError(t, err)
	require.Equal(t, user.Name, fetchedUser.Name)
	require.Equal(t, user.Bio, fetchedUser.Bio)

	// Test saving a new user with SaveUser
	newUser := &wakey.User{
		ID:   999,
		Name: "New User",
		Bio:  "New Bio",
		Tz:   0,
	}
	err = db.SaveUser(newUser)
	require.NoError(t, err)

	// Verify the new user was inserted
	fetchedNewUser, err := db.GetUserByID(999)
	require.NoError(t, err)
	require.Equal(t, newUser.Name, fetchedNewUser.Name)
	require.Equal(t, newUser.Bio, fetchedNewUser.Bio)

	// Test getting a non-existent user
	_, err = db.GetUserByID(1000)
	require.Error(t, err)
	require.Equal(t, wakey.ErrNotFound, err)
}

func TestGetAllUsers(t *testing.T) {
	db := setupTestDB(t)
	emptyUsers, err := db.GetAllUsers()
	require.NoError(t, err)
	require.Empty(t, emptyUsers)

	// Create multiple users
	users := []*wakey.User{
		{ID: 10, Name: "User 1", Bio: "Bio 1", Tz: 0},
		{ID: 11, Name: "User 2", Bio: "Bio 2", Tz: 1},
		{ID: 12, Name: "User 3", Bio: "Bio 3", Tz: 2},
	}

	for _, user := range users {
		err := db.CreateUser(user)
		require.NoError(t, err)
	}

	// Test GetAllUsers
	fetchedUsers, err := db.GetAllUsers()
	require.NoError(t, err)
	require.Len(t, fetchedUsers, len(users))

	// Verify that all created users are present in the fetched results
	for _, createdUser := range users {
		found := false
		for _, fetchedUser := range fetchedUsers {
			if fetchedUser.ID == createdUser.ID {
				require.Equal(t, createdUser.Name, fetchedUser.Name)
				require.Equal(t, createdUser.Bio, fetchedUser.Bio)
				require.Equal(t, createdUser.Tz, fetchedUser.Tz)
				found = true
				break
			}
		}
		require.True(t, found, "Created user with ID %d not found in fetched results", createdUser.ID)
	}
}

func TestPlanOperations(t *testing.T) {
	db := setupTestDB(t)

	user := &wakey.User{ID: 2, Name: "Plan User"}
	err := db.CreateUser(user)
	require.NoError(t, err)

	plan := &wakey.Plan{
		UserID:  2,
		Content: "Test Plan",
		WakeAt:  time.Now().Add(24 * time.Hour),
	}

	err = db.SavePlan(plan)
	require.NotZero(t, plan.ID)
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
	err := db.CreateUser(user)
	require.NoError(t, err)

	// Create a past plan first
	pastPlan := &wakey.Plan{
		UserID:  3,
		Content: "Past Plan",
		WakeAt:  time.Now().Add(-24 * time.Hour),
	}
	err = db.SavePlan(pastPlan)
	require.NoError(t, err)

	// This should create a copy of the past plan
	latestPlan, err := db.CopyPlanForNextDay(3)
	require.NoError(t, err)
	require.NotNil(t, latestPlan)
	require.NotZero(t, latestPlan.ID)
	require.Equal(t, pastPlan.Content, latestPlan.Content)
	require.True(t, latestPlan.WakeAt.After(time.Now()))

	plans, err := db.GetAllPlansForUser(3)
	require.NoError(t, err)
	require.Len(t, plans, 2) // Should have two plans (past and new copy)

	// Add a future plan
	futurePlan := &wakey.Plan{
		UserID:  3,
		Content: "Future Plan",
		WakeAt:  time.Now().Add(48 * time.Hour),
	}
	err = db.SavePlan(futurePlan)
	require.NoError(t, err)

	// Trying to copy when there's a future plan should return that future plan
	latestPlan, err = db.CopyPlanForNextDay(3)
	require.NoError(t, err)
	require.NotNil(t, latestPlan)
	require.Equal(t, futurePlan.Content, latestPlan.Content)
	require.Equal(t, futurePlan.ID, latestPlan.ID)

	plans, err = db.GetAllPlansForUser(3)
	require.NoError(t, err)
	require.Len(t, plans, 3) // Should still have three plans (past, copy, and future)

	// Test with non-existent user
	latestPlan, err = db.CopyPlanForNextDay(999)
	require.Error(t, err)
	require.Equal(t, wakey.ErrNotFound, err)
	require.Nil(t, latestPlan)
}

func TestWishSubscriptions(t *testing.T) {
    db := setupTestDB(t)

    // Create test data
    user := &wakey.User{ID: 80, Name: "Sub Test User"}
    err := db.CreateUser(user)
    require.NoError(t, err)

    plan := &wakey.Plan{
        UserID:  user.ID,
        Content: "Sub Test Plan",
        WakeAt:  time.Now().Add(24 * time.Hour),
    }
    err = db.SavePlan(plan)
    require.NoError(t, err)

    // Create multiple subscribers
    ch1, unsub1 := db.SubscribeToWishes(10)
    defer unsub1()

    ch2, unsub2 := db.SubscribeToWishes(10)
    defer unsub2()

    // Create channels to signal test completion
    done1 := make(chan bool)
    done2 := make(chan bool)

    // Start goroutines to listen for wish notifications
    var received1, received2 *wakey.Wish
    go func() {
        select {
        case wish := <-ch1:
            received1 = wish
            done1 <- true
        case <-time.After(2 * time.Second):
            done1 <- false
        }
    }()

    go func() {
        select {
        case wish := <-ch2:
            received2 = wish
            done2 <- true
        case <-time.After(2 * time.Second):
            done2 <- false
        }
    }()

    // Create a new wish
    wish := &wakey.Wish{
        FromID:  81,
        PlanID:  plan.ID,
        Content: "Sub Test Wish",
    }
    err = db.SaveWish(wish)
    require.NoError(t, err)

    // Wait for notifications
    success1 := <-done1
    success2 := <-done2

    require.True(t, success1, "Subscriber 1 did not receive notification")
    require.True(t, success2, "Subscriber 2 did not receive notification")

    require.Equal(t, wish.ID, received1.ID)
    require.Equal(t, wish.ID, received2.ID)

    // Test unsubscribe
    unsub1()

    // Create another wish
    wish2 := &wakey.Wish{
        FromID:  81,
        PlanID:  plan.ID,
        Content: "Sub Test Wish 2",
    }
    err = db.SaveWish(wish2)
    require.NoError(t, err)

    // Only subscriber 2 should receive it
    select {
    case <-ch1:
        t.Error("Unsubscribed channel should not receive notifications")
    case wish := <-ch2:
        require.Equal(t, wish2.ID, wish.ID)
    case <-time.After(2 * time.Second):
        t.Error("Subscriber 2 did not receive second notification")
    }
}

func TestWishOperations(t *testing.T) {
	db := setupTestDB(t)

	user := &wakey.User{ID: 4, Name: "Wish User"}
	err := db.CreateUser(user)
	require.NoError(t, err)

	plan := &wakey.Plan{
		UserID:  4,
		Content: "Wish Plan",
		WakeAt:  time.Now().Add(24 * time.Hour),
	}
	err = db.SavePlan(plan)
	require.NoError(t, err)

	// Subscribe to wish notifications
	wishChan, unsub := db.SubscribeToWishes(10)
	defer unsub()

	// Create channel to check notification
	notified := make(chan bool)
	go func() {
		select {
		case <-wishChan:
			notified <- true
		case <-time.After(time.Second):
			notified <- false
		}
	}()

	wish := &wakey.Wish{
		FromID:  5,
		PlanID:  plan.ID,
		Content: "Test Wish",
	}

	err = db.SaveWish(wish)
	require.NoError(t, err)

	// Verify notification was received
	wasNotified := <-notified
	require.True(t, wasNotified, "Wish creation notification not received")

	fetchedWish, err := db.GetWishByID(wish.ID)
	require.NoError(t, err)
	require.Equal(t, wish.Content, fetchedWish.Content)

	_, err = db.GetWishByID(999)
	require.Error(t, err)
}

func TestWishStateOperations(t *testing.T) {
	db := setupTestDB(t)

	// Create a user and plan
	user := &wakey.User{ID: 40, Name: "Wish State User"}
	err := db.CreateUser(user)
	require.NoError(t, err)

	plan := &wakey.Plan{
		UserID:  40,
		Content: "Wish State Plan",
		WakeAt:  time.Now().Add(24 * time.Hour),
	}
	err = db.SavePlan(plan)
	require.NoError(t, err)

	// Create a wish
	wish := &wakey.Wish{
		FromID:  41,
		PlanID:  plan.ID,
		Content: "Test Wish State",
	}
	err = db.SaveWish(wish)
	require.NoError(t, err)

	// Verify default state is "N" (new)
	fetchedWish, err := db.GetWishByID(wish.ID)
	require.NoError(t, err)
	require.Equal(t, wakey.WishStateNew, fetchedWish.State)

	// Test updating state
	states := []wakey.WishState{
		wakey.WishStateSent,     // "S"
		wakey.WishStateLiked,    // "L"
		wakey.WishStateDisliked, // "D"
		wakey.WishStateReported, // "R"
	}

	for _, state := range states {
		err = db.UpdateWishState(wish.ID, state)
		require.NoError(t, err)

		fetchedWish, err = db.GetWishByID(wish.ID)
		require.NoError(t, err)
		require.Equal(t, state, fetchedWish.State)
	}

	// Test updating non-existent wish
	err = db.UpdateWishState(999, wakey.WishStateLiked)
	require.Error(t, err)
	require.Equal(t, wakey.ErrNotFound, err)
}

func TestFindPlanForWish(t *testing.T) {
	db := setupTestDB(t)

	user1 := &wakey.User{ID: 6, Name: "Wish User 1"}
	user2 := &wakey.User{ID: 7, Name: "Wish User 2"}
	err := db.CreateUser(user1)
	require.NoError(t, err)
	err = db.CreateUser(user2)
	require.NoError(t, err)

	// Test finding a plan when none are available
	_, err = db.FindPlanForWish(6)
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
	foundPlan, err := db.FindPlanForWish(6)
	require.NoError(t, err)
	require.NotNil(t, foundPlan)
	require.Equal(t, int64(7), foundPlan.UserID)

	// Test with non-existent sender ID (should still find a plan)
	nonExistentSenderID := int64(999)
	foundPlan, err = db.FindPlanForWish(nonExistentSenderID)
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
	_, err = db.FindPlanForWish(6)
	require.Error(t, err)
	require.Equal(t, wakey.ErrNotFound, err)
}

func TestGetFuturePlans(t *testing.T) {
	db := setupTestDB(t)

	// Create users
	users := []*wakey.User{
		{ID: 20, Name: "Future Plan User 1"},
		{ID: 21, Name: "Future Plan User 2"},
	}
	for _, user := range users {
		err := db.CreateUser(user)
		require.NoError(t, err)
	}

	now := time.Now()

	// Create various plans
	plans := []*wakey.Plan{
		{
			UserID:  20,
			Content: "Past Plan 1",
			WakeAt:  now.Add(-24 * time.Hour),
		},
		{
			UserID:  20,
			Content: "Future Plan 1",
			WakeAt:  now.Add(24 * time.Hour),
		},
		{
			UserID:  21,
			Content: "Past Plan 2",
			WakeAt:  now.Add(-48 * time.Hour),
		},
		{
			UserID:  21,
			Content: "Future Plan 2",
			WakeAt:  now.Add(48 * time.Hour),
		},
	}

	for _, plan := range plans {
		err := db.SavePlan(plan)
		require.NoError(t, err)
	}

	// Test GetFuturePlans
	futurePlans, err := db.GetFuturePlans()
	require.NoError(t, err)

	// Should only get the future plans
	require.Len(t, futurePlans, 2)

	// Verify the returned plans are actually in the future
	for _, plan := range futurePlans {
		require.True(t, plan.WakeAt.After(now))
	}

	// Verify the content of future plans
	futureContents := make(map[string]bool)
	for _, plan := range futurePlans {
		futureContents[plan.Content] = true
	}
	require.True(t, futureContents["Future Plan 1"])
	require.True(t, futureContents["Future Plan 2"])
}

func TestGetNewWishesByUserID(t *testing.T) {
	db := setupTestDB(t)

	// Create users
	user1 := &wakey.User{ID: 50, Name: "Wishes User 1"}
	user2 := &wakey.User{ID: 51, Name: "Wishes User 2"}
	err := db.CreateUser(user1)
	require.NoError(t, err)
	err = db.CreateUser(user2)
	require.NoError(t, err)

	// Create plans for both users
	plan1 := &wakey.Plan{
		UserID:  user1.ID,
		Content: "Plan 1",
		WakeAt:  time.Now().Add(24 * time.Hour),
	}
	plan2 := &wakey.Plan{
		UserID:  user1.ID,
		Content: "Plan 2",
		WakeAt:  time.Now().Add(48 * time.Hour),
	}
	plan3 := &wakey.Plan{
		UserID:  user2.ID,
		Content: "Plan 3",
		WakeAt:  time.Now().Add(24 * time.Hour),
	}

	err = db.SavePlan(plan1)
	require.NoError(t, err)
	err = db.SavePlan(plan2)
	require.NoError(t, err)
	err = db.SavePlan(plan3)
	require.NoError(t, err)

	// Create various wishes with different states
	wishes := []*wakey.Wish{
		{
			FromID:  60,
			PlanID:  plan1.ID,
			Content: "New Wish 1",
			State:   wakey.WishStateNew,
		},
		{
			FromID:  61,
			PlanID:  plan1.ID,
			Content: "Sent Wish",
			State:   wakey.WishStateSent,
		},
		{
			FromID:  62,
			PlanID:  plan2.ID,
			Content: "New Wish 2",
			State:   wakey.WishStateNew,
		},
		{
			FromID:  63,
			PlanID:  plan3.ID,
			Content: "New Wish 3",
			State:   wakey.WishStateNew,
		},
	}

	for _, wish := range wishes {
		err := db.SaveWish(wish)
		require.NoError(t, err)
	}

	// Test getting new wishes for user1
	newWishes, err := db.GetNewWishesByUserID(user1.ID)
	require.NoError(t, err)
	require.Len(t, newWishes, 2) // Should only get the new wishes for user1's plans

	// Verify the contents
	wishContents := make(map[string]bool)
	for _, wish := range newWishes {
		wishContents[wish.Content] = true
		require.Equal(t, wakey.WishStateNew, wish.State)
	}
	require.True(t, wishContents["New Wish 1"])
	require.True(t, wishContents["New Wish 2"])
	require.False(t, wishContents["Sent Wish"])
	require.False(t, wishContents["New Wish 3"])

	// Test getting new wishes for user2
	newWishes, err = db.GetNewWishesByUserID(user2.ID)
	require.NoError(t, err)
	require.Len(t, newWishes, 1)
	require.Equal(t, "New Wish 3", newWishes[0].Content)

	// Test getting new wishes for non-existent user
	newWishes, err = db.GetNewWishesByUserID(999)
	require.NoError(t, err)
	require.Empty(t, newWishes)
}

func TestWishToxicityOperations(t *testing.T) {
	db := setupTestDB(t)

	// Create a user and plan
	user := &wakey.User{ID: 70, Name: "Toxicity Test User"}
	err := db.CreateUser(user)
	require.NoError(t, err)

	plan := &wakey.Plan{
		UserID:  user.ID,
		Content: "Toxicity Test Plan",
		WakeAt:  time.Now().Add(24 * time.Hour),
	}
	err = db.SavePlan(plan)
	require.NoError(t, err)

	// Create wishes with different toxicity values
	wishes := []*wakey.Wish{
		{
			FromID:  71,
			PlanID:  plan.ID,
			Content: "Unrated Wish 1",
			// Toxicity is NULL by default
		},
		{
			FromID:  72,
			PlanID:  plan.ID,
			Content: "Rated Wish",
			Toxicity: sql.NullInt16{
				Int16: 75,
				Valid: true,
			},
		},
		{
			FromID:  73,
			PlanID:  plan.ID,
			Content: "Zero Toxicity Wish",
			Toxicity: sql.NullInt16{
				Int16: 0,
				Valid: true,
			},
		},
		{
			FromID:  74,
			PlanID:  plan.ID,
			Content: "Unrated Wish 2",
			// Toxicity is NULL by default
		},
	}

	for _, wish := range wishes {
		err := db.SaveWish(wish)
		require.NoError(t, err)
	}

	// Test GetUnratedWishes
	unratedWishes, err := db.GetUnratedWishes()
	require.NoError(t, err)
	require.Len(t, unratedWishes, 2)

	// Verify unrated wishes content
	unratedContents := make(map[string]bool)
	for _, wish := range unratedWishes {
		unratedContents[wish.Content] = true
		require.False(t, wish.Toxicity.Valid)
	}
	require.True(t, unratedContents["Unrated Wish 1"])
	require.True(t, unratedContents["Unrated Wish 2"])
	require.False(t, unratedContents["Rated Wish"])
	require.False(t, unratedContents["Zero Toxicity Wish"])

	// Test UpdateWishToxicity
	err = db.UpdateWishToxicity(unratedWishes[0].ID, 50)
	require.NoError(t, err)

	// Verify the update
	updatedWish, err := db.GetWishByID(unratedWishes[0].ID)
	require.NoError(t, err)
	require.True(t, updatedWish.Toxicity.Valid)
	require.Equal(t, int16(50), updatedWish.Toxicity.Int16)

	// Test updating non-existent wish
	err = db.UpdateWishToxicity(999, 50)
	require.Error(t, err)
	require.Equal(t, wakey.ErrNotFound, err)

	// Verify only one unrated wish remains
	unratedWishes, err = db.GetUnratedWishes()
	require.NoError(t, err)
	require.Len(t, unratedWishes, 1)
	require.Equal(t, "Unrated Wish 2", unratedWishes[0].Content)

	// Test setting toxicity to zero
	err = db.UpdateWishToxicity(unratedWishes[0].ID, 0)
	require.NoError(t, err)

	// Verify zero toxicity is different from unrated
	unratedWishes, err = db.GetUnratedWishes()
	require.NoError(t, err)
	require.Len(t, unratedWishes, 0)
}

func TestGetStats(t *testing.T) {
	db := setupTestDB(t)

	// Create test users
	users := []*wakey.User{
		{ID: 30, Name: "Stats User 1"},
		{ID: 31, Name: "Stats User 2"},
		{ID: 32, Name: "Stats User 3"},
	}
	for _, user := range users {
		err := db.CreateUser(user)
		require.NoError(t, err)
	}

	// Create some plans
	plans := []*wakey.Plan{
		{
			UserID:  30,
			Content: "Stats Plan 1",
			WakeAt:  time.Now().Add(24 * time.Hour),
		},
		{
			UserID:  31,
			Content: "Stats Plan 2",
			WakeAt:  time.Now().Add(48 * time.Hour),
		},
	}
	for _, plan := range plans {
		err := db.SavePlan(plan)
		require.NoError(t, err)
	}

	// Create some wishes
	wishes := []*wakey.Wish{
		{
			FromID:  32,
			PlanID:  plans[0].ID,
			Content: "Stats Wish 1",
		},
		{
			FromID:  31,
			PlanID:  plans[0].ID,
			Content: "Stats Wish 2",
		},
	}
	for _, wish := range wishes {
		err := db.SaveWish(wish)
		require.NoError(t, err)
	}

	// Get stats
	stats, err := db.GetStats()
	require.NoError(t, err)
	require.NotNil(t, stats)

	// Verify counts
	require.Equal(t, int64(3), stats.TotalUsers)
	require.Equal(t, int64(2), stats.TotalPlans)
	require.Equal(t, int64(2), stats.TotalWishes)

	// All users are new and active in last 7 days
	require.Equal(t, int64(3), stats.NewUsersLast7Days)
	require.Equal(t, int64(3), stats.ActiveUsersLast7Days)

	// Average plans and wishes per day
	require.InDelta(t, 2.0/7.0, stats.AvgPlansLast7Days, 0.001)
	require.InDelta(t, 2.0/7.0, stats.AvgWishesLast7Days, 0.001)
}

func TestGetStatsWithLikedWishes(t *testing.T) {
	db := setupTestDB(t)

	// Create test users
	users := []*wakey.User{
		{ID: 30, Name: "Stats User 1"},
		{ID: 31, Name: "Stats User 2"},
	}
	for _, user := range users {
		err := db.CreateUser(user)
		require.NoError(t, err)
	}

	// Create a plan
	plan := &wakey.Plan{
		UserID:  30,
		Content: "Stats Plan 1",
		WakeAt:  time.Now().Add(24 * time.Hour),
	}
	err := db.SavePlan(plan)
	require.NoError(t, err)

	// Create wishes with different states
	wishes := []*wakey.Wish{
		{
			FromID:  31,
			PlanID:  plan.ID,
			Content: "Liked Wish 1",
			State:   wakey.WishStateLiked,
		},
		{
			FromID:  31,
			PlanID:  plan.ID,
			Content: "New Wish",
			State:   wakey.WishStateNew,
		},
		{
			FromID:  31,
			PlanID:  plan.ID,
			Content: "Liked Wish 2",
			State:   wakey.WishStateLiked,
		},
		{
			FromID:  31,
			PlanID:  plan.ID,
			Content: "Disliked Wish",
			State:   wakey.WishStateDisliked,
		},
	}

	for _, wish := range wishes {
		err := db.SaveWish(wish)
		require.NoError(t, err)
	}

	// Get stats
	stats, err := db.GetStats()
	require.NoError(t, err)
	require.NotNil(t, stats)

	// Verify liked wishes stats
	require.Equal(t, int64(2), stats.TotalLikedWishes)
	require.InDelta(t, 50.0, stats.LikedWishesPercent, 0.1) // 2 liked out of 4 total = 50%
	require.Equal(t, int64(2), stats.LikedWishesLast7Days)
	require.InDelta(t, 50.0, stats.LikedWishesLast7DaysPercent, 0.1)
}

func TestDBStates(t *testing.T) {
	db := setupTestDB(t)

	// Create test data
	baseTime := time.Now().UTC()
	states := map[int64]*wakey.UserData{
		1: {
			State:        wakey.StateAwaitingName,
			Name:         "John",
			Bio:          "Test bio 1",
			Plans:        "Morning plan",
			TargetPlanID: 123,
			AskAboutWish: true,
			LastUpdated:  baseTime,
		},
		2: {
			State:        wakey.StateAwaitingBio,
			Name:         "Jane",
			Bio:          "Test bio 2",
			Plans:        "Evening plan",
			TargetPlanID: 456,
			AskAboutWish: false,
			LastUpdated:  baseTime,
		},
	}

	// Test saving states
	err := db.SaveStates(states)
	require.NoError(t, err)

	// Test loading states
	loadedStates, err := db.LoadStates()
	require.NoError(t, err)
	require.Equal(t, len(states), len(loadedStates))

	// Verify loaded data
	for userID, expectedData := range states {
		loadedData, exists := loadedStates[userID]
		require.True(t, exists)
		require.Equal(t, expectedData.State, loadedData.State)
		require.Equal(t, expectedData.Name, loadedData.Name)
		require.Equal(t, expectedData.Bio, loadedData.Bio)
		require.Equal(t, expectedData.Plans, loadedData.Plans)
		require.Equal(t, expectedData.TargetPlanID, loadedData.TargetPlanID)
		require.Equal(t, expectedData.AskAboutWish, loadedData.AskAboutWish)
		require.WithinDuration(t, expectedData.LastUpdated, loadedData.LastUpdated, time.Second)
	}

	// Test overwriting states
	newStates := map[int64]*wakey.UserData{
		3: {
			State:       wakey.StateAwaitingPlans,
			Name:        "Bob",
			LastUpdated: baseTime,
		},
	}

	err = db.SaveStates(newStates)
	require.NoError(t, err)

	loadedStates, err = db.LoadStates()
	require.NoError(t, err)
	require.Equal(t, 1, len(loadedStates))
	require.Contains(t, loadedStates, int64(3))
}
