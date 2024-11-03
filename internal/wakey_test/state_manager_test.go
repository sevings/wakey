package wakey_test

import (
	"testing"
	"time"

	"wakey/internal/wakey"

	"github.com/stretchr/testify/require"
)

func TestStateManager(t *testing.T) {
	t.Run("SetState and GetState", func(t *testing.T) {
		sm := wakey.NewStateManager()
		userID := int64(1)

		sm.SetState(userID, wakey.StateAwaitingName)
		state, exists := sm.GetState(userID)
		require.True(t, exists)
		require.Equal(t, wakey.StateAwaitingName, state)

		state, exists = sm.GetState(int64(2))
		require.False(t, exists)
		require.Equal(t, wakey.StateNone, state)
	})

	t.Run("SetUserData and GetUserData", func(t *testing.T) {
		sm := wakey.NewStateManager()
		userID := int64(1)
		userData := &wakey.UserData{
			State: wakey.StateAwaitingBio,
			Name:  "John Doe",
		}

		sm.SetUserData(userID, userData)
		retrievedData, exists := sm.GetUserData(userID)
		require.True(t, exists)
		require.Equal(t, userData.State, retrievedData.State)
		require.Equal(t, userData.Name, retrievedData.Name)

		_, exists = sm.GetUserData(int64(2))
		require.False(t, exists)
	})

	t.Run("ClearState", func(t *testing.T) {
		sm := wakey.NewStateManager()
		userID := int64(1)

		sm.SetState(userID, wakey.StateAwaitingName)
		sm.ClearState(userID)

		_, exists := sm.GetState(userID)
		require.False(t, exists)
	})

	t.Run("CleanupOldStates", func(t *testing.T) {
		sm := wakey.NewStateManager()
		userID1 := int64(1)
		userID2 := int64(2)

		baseTime := time.Now()
		testNow := baseTime

		sm.SetNowFunc(func() time.Time {
			return testNow
		})

		sm.SetState(userID1, wakey.StateAwaitingName)
		sm.SetState(userID2, wakey.StateAwaitingBio)

		// Verify both states exist
		_, exists := sm.GetState(userID1)
		require.True(t, exists)
		_, exists = sm.GetState(userID2)
		require.True(t, exists)

		// Move time forward by 30 minutes
		testNow = baseTime.Add(30 * time.Minute)

		// This should not clean up any states
		sm.CleanupOldStates(time.Hour)

		_, exists = sm.GetState(userID1)
		require.True(t, exists)
		_, exists = sm.GetState(userID2)
		require.True(t, exists)

		// Move time forward by 2 hours
		testNow = baseTime.Add(2 * time.Hour)

		// This should clean up all states
		sm.CleanupOldStates(time.Hour)

		_, exists = sm.GetState(userID1)
		require.False(t, exists)
		_, exists = sm.GetState(userID2)
		require.False(t, exists)
	})

	t.Run("Start and Stop", func(t *testing.T) {
		sm := wakey.NewStateManager()
		sm.Start(time.Minute, time.Hour)

		// Wait a bit to ensure the cleanup routine has started
		time.Sleep(100 * time.Millisecond)

		sm.Stop()
		// Ensure Stop can be called multiple times without panic
		sm.Stop()

		// Try to start again after stopping (should not panic)
		sm.Start(time.Minute, time.Hour)
	})

	t.Run("ImportStates and ExportStates", func(t *testing.T) {
		sm := wakey.NewStateManager()
		baseTime := time.Now()

		// Create test data
		initialStates := map[int64]*wakey.UserData{
			1: {
				State:        wakey.StateAwaitingName,
				Name:         "John",
				Bio:          "Test bio 1",
				LastUpdated:  baseTime,
				AskAboutWish: true,
			},
			2: {
				State:        wakey.StateAwaitingBio,
				Name:         "Jane",
				Bio:          "Test bio 2",
				LastUpdated:  baseTime,
				AskAboutWish: false,
			},
		}

		// Test import before start
		err := sm.ImportStates(initialStates)
		require.NoError(t, err)

		// Verify imported states
		for userID, expectedData := range initialStates {
			userData, exists := sm.GetUserData(userID)
			require.True(t, exists)
			require.Equal(t, expectedData.State, userData.State)
			require.Equal(t, expectedData.Name, userData.Name)
			require.Equal(t, expectedData.Bio, userData.Bio)
			require.Equal(t, expectedData.AskAboutWish, userData.AskAboutWish)
		}

		// Test import after start (should fail)
		sm.Start(time.Minute, time.Hour)
		err = sm.ImportStates(initialStates)
		require.Error(t, err)

		// Test export before stop (should fail)
		_, err = sm.ExportStates()
		require.Error(t, err)

		// Stop the manager
		sm.Stop()

		// Test export after stop
		exportedStates, err := sm.ExportStates()
		require.NoError(t, err)

		// Verify exported states match the initial states
		require.Equal(t, len(initialStates), len(exportedStates))
		for userID, expectedData := range initialStates {
			exportedData, exists := exportedStates[userID]
			require.True(t, exists)
			require.Equal(t, expectedData.State, exportedData.State)
			require.Equal(t, expectedData.Name, exportedData.Name)
			require.Equal(t, expectedData.Bio, exportedData.Bio)
			require.Equal(t, expectedData.AskAboutWish, exportedData.AskAboutWish)
		}

		// Test data isolation
		exportedStates[1].Name = "Modified Name"
		userData, exists := sm.GetUserData(int64(1))
		require.True(t, exists)
		require.NotEqual(t, "Modified Name", userData.Name)
	})

	t.Run("Import empty states", func(t *testing.T) {
		sm := wakey.NewStateManager()

		err := sm.ImportStates(make(map[int64]*wakey.UserData))
		require.NoError(t, err)

		sm.Stop()
		exported, err := sm.ExportStates()
		require.NoError(t, err)
		require.Empty(t, exported)
	})

	t.Run("Import nil states", func(t *testing.T) {
		sm := wakey.NewStateManager()

		err := sm.ImportStates(nil)
		require.NoError(t, err)

		sm.Stop()
		exported, err := sm.ExportStates()
		require.NoError(t, err)
		require.Empty(t, exported)
	})
}
