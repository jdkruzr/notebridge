package syncdb

import (
	"context"
	"testing"
)

// TestCreateSummary with Snowflake ID.
func TestCreateSummary(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()

	// Create a user
	err := store.EnsureUser(ctx, "test@example.com", "hash", 1000000000000001)
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	user, err := store.GetUserByEmail(ctx, "test@example.com")
	if err != nil {
		t.Fatalf("failed to get user: %v", err)
	}

	// Create summary
	summary := &Summary{
		ID:               123456,
		UserID:           user.ID,
		UniqueIdentifier: "unique1",
		Name:             "My Summary",
		Description:      "A test summary",
		Content:          "Summary content",
		IsSummaryGroup:   "N",
		CreationTime:     1000,
		LastModifiedTime: 1100,
	}

	err = store.CreateSummary(ctx, summary)
	if err != nil {
		t.Fatalf("failed to create summary: %v", err)
	}

	// Verify it was created
	retrieved, err := store.GetSummary(ctx, 123456, user.ID)
	if err != nil {
		t.Fatalf("failed to get summary: %v", err)
	}

	if retrieved == nil {
		t.Fatalf("summary not found")
	}

	if retrieved.Name != "My Summary" {
		t.Errorf("expected name 'My Summary', got '%s'", retrieved.Name)
	}
}

// TestCreateSummaryDuplicateUniqueID returns error.
func TestCreateSummaryDuplicateUniqueID(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()

	// Create a user
	err := store.EnsureUser(ctx, "test@example.com", "hash", 1000000000000001)
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	user, err := store.GetUserByEmail(ctx, "test@example.com")
	if err != nil {
		t.Fatalf("failed to get user: %v", err)
	}

	// Create first summary
	summary1 := &Summary{
		ID:               123456,
		UserID:           user.ID,
		UniqueIdentifier: "unique1",
		Name:             "Summary 1",
		IsSummaryGroup:   "N",
		CreationTime:     1000,
		LastModifiedTime: 1100,
	}

	err = store.CreateSummary(ctx, summary1)
	if err != nil {
		t.Fatalf("failed to create summary 1: %v", err)
	}

	// Try to create second summary with same unique_identifier
	summary2 := &Summary{
		ID:               123457,
		UserID:           user.ID,
		UniqueIdentifier: "unique1",
		Name:             "Summary 2",
		IsSummaryGroup:   "N",
		CreationTime:     1000,
		LastModifiedTime: 1100,
	}

	err = store.CreateSummary(ctx, summary2)

	if err != ErrUniqueIDExists {
		t.Errorf("expected ErrUniqueIDExists, got %v", err)
	}
}

// TestUpdateSummary partial update.
func TestUpdateSummary(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()

	// Create a user
	err := store.EnsureUser(ctx, "test@example.com", "hash", 1000000000000001)
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	user, err := store.GetUserByEmail(ctx, "test@example.com")
	if err != nil {
		t.Fatalf("failed to get user: %v", err)
	}

	// Create summary
	summary := &Summary{
		ID:               123456,
		UserID:           user.ID,
		UniqueIdentifier: "unique1",
		Name:             "My Summary",
		IsSummaryGroup:   "N",
		CreationTime:     1000,
		LastModifiedTime: 1100,
	}

	err = store.CreateSummary(ctx, summary)
	if err != nil {
		t.Fatalf("failed to create summary: %v", err)
	}

	// Update it
	err = store.UpdateSummary(ctx, 123456, user.ID, map[string]interface{}{"name": "Updated Summary"})
	if err != nil {
		t.Fatalf("failed to update summary: %v", err)
	}

	// Verify update
	retrieved, err := store.GetSummary(ctx, 123456, user.ID)
	if err != nil {
		t.Fatalf("failed to get summary: %v", err)
	}

	if retrieved.Name != "Updated Summary" {
		t.Errorf("expected updated name 'Updated Summary', got '%s'", retrieved.Name)
	}
}

// TestUpdateSummaryNotFound returns error.
func TestUpdateSummaryNotFound(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()

	// Create a user
	err := store.EnsureUser(ctx, "test@example.com", "hash", 1000000000000001)
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	user, err := store.GetUserByEmail(ctx, "test@example.com")
	if err != nil {
		t.Fatalf("failed to get user: %v", err)
	}

	// Try to update non-existent summary
	err = store.UpdateSummary(ctx, 999999, user.ID, map[string]interface{}{"name": "New Name"})

	if err != ErrSummaryNotFound {
		t.Errorf("expected ErrSummaryNotFound, got %v", err)
	}
}

// TestDeleteSummary removes summary.
func TestDeleteSummary(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()

	// Create a user
	err := store.EnsureUser(ctx, "test@example.com", "hash", 1000000000000001)
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	user, err := store.GetUserByEmail(ctx, "test@example.com")
	if err != nil {
		t.Fatalf("failed to get user: %v", err)
	}

	// Create summary
	summary := &Summary{
		ID:               123456,
		UserID:           user.ID,
		UniqueIdentifier: "unique1",
		Name:             "My Summary",
		IsSummaryGroup:   "N",
		CreationTime:     1000,
		LastModifiedTime: 1100,
	}

	err = store.CreateSummary(ctx, summary)
	if err != nil {
		t.Fatalf("failed to create summary: %v", err)
	}

	// Delete it
	err = store.DeleteSummary(ctx, 123456, user.ID)
	if err != nil {
		t.Fatalf("failed to delete summary: %v", err)
	}

	// Verify it's gone
	retrieved, err := store.GetSummary(ctx, 123456, user.ID)
	if err != nil {
		t.Fatalf("failed to get summary: %v", err)
	}

	if retrieved != nil {
		t.Errorf("expected summary to be deleted, but found it")
	}
}

// TestListSummaryGroups returns only is_summary_group='Y'.
func TestListSummaryGroups(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()

	// Create a user
	err := store.EnsureUser(ctx, "test@example.com", "hash", 1000000000000001)
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	user, err := store.GetUserByEmail(ctx, "test@example.com")
	if err != nil {
		t.Fatalf("failed to get user: %v", err)
	}

	// Create a group and a non-group
	group := &Summary{
		ID:               123456,
		UserID:           user.ID,
		UniqueIdentifier: "group1",
		Name:             "My Group",
		IsSummaryGroup:   "Y",
		CreationTime:     1000,
		LastModifiedTime: 1100,
	}

	err = store.CreateSummary(ctx, group)
	if err != nil {
		t.Fatalf("failed to create group: %v", err)
	}

	nonGroup := &Summary{
		ID:               123457,
		UserID:           user.ID,
		UniqueIdentifier: "item1",
		Name:             "My Item",
		IsSummaryGroup:   "N",
		CreationTime:     1000,
		LastModifiedTime: 1100,
	}

	err = store.CreateSummary(ctx, nonGroup)
	if err != nil {
		t.Fatalf("failed to create item: %v", err)
	}

	// List groups
	groups, count, err := store.ListSummaryGroups(ctx, user.ID, 1, 10)
	if err != nil {
		t.Fatalf("failed to list groups: %v", err)
	}

	if count != 1 {
		t.Errorf("expected 1 group, got %d", count)
	}

	if len(groups) != 1 {
		t.Errorf("expected 1 group in result, got %d", len(groups))
	}

	if groups[0].Name != "My Group" {
		t.Errorf("expected group name 'My Group', got '%s'", groups[0].Name)
	}
}

// TestListSummaries returns only is_summary_group='N'.
func TestListSummaries(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()

	// Create a user
	err := store.EnsureUser(ctx, "test@example.com", "hash", 1000000000000001)
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	user, err := store.GetUserByEmail(ctx, "test@example.com")
	if err != nil {
		t.Fatalf("failed to get user: %v", err)
	}

	// Create a group and a non-group
	group := &Summary{
		ID:               123456,
		UserID:           user.ID,
		UniqueIdentifier: "group1",
		Name:             "My Group",
		IsSummaryGroup:   "Y",
		CreationTime:     1000,
		LastModifiedTime: 1100,
	}

	err = store.CreateSummary(ctx, group)
	if err != nil {
		t.Fatalf("failed to create group: %v", err)
	}

	nonGroup := &Summary{
		ID:               123457,
		UserID:           user.ID,
		UniqueIdentifier: "item1",
		Name:             "My Item",
		IsSummaryGroup:   "N",
		CreationTime:     1000,
		LastModifiedTime: 1100,
	}

	err = store.CreateSummary(ctx, nonGroup)
	if err != nil {
		t.Fatalf("failed to create item: %v", err)
	}

	// List non-groups
	items, count, err := store.ListSummaries(ctx, user.ID, 1, 10, nil)
	if err != nil {
		t.Fatalf("failed to list summaries: %v", err)
	}

	if count != 1 {
		t.Errorf("expected 1 item, got %d", count)
	}

	if len(items) != 1 {
		t.Errorf("expected 1 item in result, got %d", len(items))
	}

	if items[0].Name != "My Item" {
		t.Errorf("expected item name 'My Item', got '%s'", items[0].Name)
	}
}

// TestListSummariesWithParentFilter filters by parent_unique_identifier.
func TestListSummariesWithParentFilter(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()

	// Create a user
	err := store.EnsureUser(ctx, "test@example.com", "hash", 1000000000000001)
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	user, err := store.GetUserByEmail(ctx, "test@example.com")
	if err != nil {
		t.Fatalf("failed to get user: %v", err)
	}

	// Create parent group
	parentGroup := &Summary{
		ID:               123456,
		UserID:           user.ID,
		UniqueIdentifier: "parent1",
		Name:             "Parent Group",
		IsSummaryGroup:   "Y",
		CreationTime:     1000,
		LastModifiedTime: 1100,
	}

	err = store.CreateSummary(ctx, parentGroup)
	if err != nil {
		t.Fatalf("failed to create parent group: %v", err)
	}

	// Create items with parent
	item1 := &Summary{
		ID:                     123457,
		UserID:                 user.ID,
		UniqueIdentifier:       "item1",
		Name:                   "Item 1",
		ParentUniqueIdentifier: "parent1",
		IsSummaryGroup:         "N",
		CreationTime:           1000,
		LastModifiedTime:       1100,
	}

	err = store.CreateSummary(ctx, item1)
	if err != nil {
		t.Fatalf("failed to create item 1: %v", err)
	}

	// Create item without parent
	item2 := &Summary{
		ID:                     123458,
		UserID:                 user.ID,
		UniqueIdentifier:       "item2",
		Name:                   "Item 2",
		ParentUniqueIdentifier: "",
		IsSummaryGroup:         "N",
		CreationTime:           1000,
		LastModifiedTime:       1100,
	}

	err = store.CreateSummary(ctx, item2)
	if err != nil {
		t.Fatalf("failed to create item 2: %v", err)
	}

	// List items with parent filter
	parentUID := "parent1"
	items, count, err := store.ListSummaries(ctx, user.ID, 1, 10, &parentUID)
	if err != nil {
		t.Fatalf("failed to list summaries with parent filter: %v", err)
	}

	if count != 1 {
		t.Errorf("expected 1 item with parent, got %d", count)
	}

	if len(items) != 1 {
		t.Errorf("expected 1 item in result, got %d", len(items))
	}

	if items[0].Name != "Item 1" {
		t.Errorf("expected item name 'Item 1', got '%s'", items[0].Name)
	}
}

// TestListSummaryHashes returns lightweight hash data.
func TestListSummaryHashes(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()

	// Create a user
	err := store.EnsureUser(ctx, "test@example.com", "hash", 1000000000000001)
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	user, err := store.GetUserByEmail(ctx, "test@example.com")
	if err != nil {
		t.Fatalf("failed to get user: %v", err)
	}

	// Create summary with hash data
	summary := &Summary{
		ID:                   123456,
		UserID:               user.ID,
		UniqueIdentifier:     "unique1",
		Name:                 "My Summary",
		MD5Hash:              "hash123",
		HandwriteMD5:         "hash456",
		CommentHandwriteName: "comment1",
		Metadata:             "metadata1",
		IsSummaryGroup:       "N",
		CreationTime:         1000,
		LastModifiedTime:     1100,
	}

	err = store.CreateSummary(ctx, summary)
	if err != nil {
		t.Fatalf("failed to create summary: %v", err)
	}

	// List hashes
	hashes, count, err := store.ListSummaryHashes(ctx, user.ID, 1, 10, nil)
	if err != nil {
		t.Fatalf("failed to list summary hashes: %v", err)
	}

	if count != 1 {
		t.Errorf("expected 1 hash, got %d", count)
	}

	if len(hashes) != 1 {
		t.Errorf("expected 1 hash in result, got %d", len(hashes))
	}

	hash := hashes[0]
	if hash.MD5Hash != "hash123" {
		t.Errorf("expected md5_hash 'hash123', got '%s'", hash.MD5Hash)
	}

	if hash.HandwriteMD5 != "hash456" {
		t.Errorf("expected handwrite_md5 'hash456', got '%s'", hash.HandwriteMD5)
	}
}

// TestGetSummariesByIDs returns matching summaries.
func TestGetSummariesByIDs(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()

	// Create a user
	err := store.EnsureUser(ctx, "test@example.com", "hash", 1000000000000001)
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	user, err := store.GetUserByEmail(ctx, "test@example.com")
	if err != nil {
		t.Fatalf("failed to get user: %v", err)
	}

	// Create 3 summaries
	for i := 1; i <= 3; i++ {
		id := int64(123450 + i)
		summary := &Summary{
			ID:               id,
			UserID:           user.ID,
			UniqueIdentifier: "unique" + string(rune('0'+i)),
			Name:             "Summary " + string(rune('0'+i)),
			IsSummaryGroup:   "N",
			CreationTime:     1000,
			LastModifiedTime: 1100,
		}

		err := store.CreateSummary(ctx, summary)
		if err != nil {
			t.Fatalf("failed to create summary: %v", err)
		}
	}

	// Get specific IDs
	ids := []int64{123451, 123453}
	results, count, err := store.GetSummariesByIDs(ctx, user.ID, ids, 1, 10)
	if err != nil {
		t.Fatalf("failed to get summaries by IDs: %v", err)
	}

	if count != 2 {
		t.Errorf("expected 2 summaries, got %d", count)
	}

	if len(results) != 2 {
		t.Errorf("expected 2 results, got %d", len(results))
	}
}

// TestPaginationWorksForAllOperations.
func TestPaginationWorksForAllOperations(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()

	// Create a user
	err := store.EnsureUser(ctx, "test@example.com", "hash", 1000000000000001)
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	user, err := store.GetUserByEmail(ctx, "test@example.com")
	if err != nil {
		t.Fatalf("failed to get user: %v", err)
	}

	// Create 5 summaries
	for i := 1; i <= 5; i++ {
		id := int64(123450 + i)
		isSummaryGroup := "N"
		if i%2 == 0 {
			isSummaryGroup = "Y"
		}

		summary := &Summary{
			ID:               id,
			UserID:           user.ID,
			UniqueIdentifier: "unique" + string(rune('0'+i)),
			Name:             "Summary " + string(rune('0'+i)),
			IsSummaryGroup:   isSummaryGroup,
			CreationTime:     1000,
			LastModifiedTime: 1100,
		}

		err := store.CreateSummary(ctx, summary)
		if err != nil {
			t.Fatalf("failed to create summary: %v", err)
		}
	}

	// ListSummaryGroups pagination
	groups, count, err := store.ListSummaryGroups(ctx, user.ID, 1, 2)
	if err != nil {
		t.Fatalf("failed to list groups (page 1): %v", err)
	}

	if count != 2 {
		t.Errorf("expected total 2 groups, got %d", count)
	}

	if len(groups) != 2 {
		t.Errorf("expected 2 groups on page 1, got %d", len(groups))
	}

	// ListSummaries pagination
	items, count, err := store.ListSummaries(ctx, user.ID, 1, 2, nil)
	if err != nil {
		t.Fatalf("failed to list summaries (page 1): %v", err)
	}

	if count != 3 {
		t.Errorf("expected total 3 items, got %d", count)
	}

	if len(items) != 2 {
		t.Errorf("expected 2 items on page 1, got %d", len(items))
	}

	// ListSummaryHashes pagination
	hashes, count, err := store.ListSummaryHashes(ctx, user.ID, 1, 2, nil)
	if err != nil {
		t.Fatalf("failed to list hashes (page 1): %v", err)
	}

	if count != 5 {
		t.Errorf("expected total 5 hashes, got %d", count)
	}

	if len(hashes) != 2 {
		t.Errorf("expected 2 hashes on page 1, got %d", len(hashes))
	}
}
