package e2e

import (
	"context"
	"fmt"
	"fullcycle-auction_go/configuration/database/mongodb"
	"fullcycle-auction_go/internal/entity/auction_entity"
	"fullcycle-auction_go/internal/infra/database/auction"
	"fullcycle-auction_go/internal/infra/database/bid"
	"fullcycle-auction_go/internal/infra/database/user"
	"fullcycle-auction_go/internal/usecase/auction_usecase"
	"fullcycle-auction_go/internal/usecase/bid_usecase"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/joho/godotenv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/mongo"
)

// TestAuctionFlow_E2E tests the complete auction workflow using Docker MongoDB
func TestAuctionFlow_E2E(t *testing.T) {
	// Load environment variables
	err := godotenv.Load("../../../cmd/auction/.env")
	require.NoError(t, err, "Failed to load .env file")

	os.Setenv("MONGODB_URL", "mongodb://admin:admin@localhost:27017/auctions_e2e_tests?authSource=admin")
	os.Setenv("MONGODB_DB", "auctions_e2e_tests")
	os.Setenv("AUCTION_INTERVAL", "5s")
	os.Setenv("MAX_BATCH_SIZE", "2")
	os.Setenv("BATCH_INSERT_INTERVAL", "2s")

	fmt.Println("🚀 Starting Simple Auction Flow E2E Test")
	fmt.Printf("⚙️  AUCTION_INTERVAL: %s\n", os.Getenv("AUCTION_INTERVAL"))
	fmt.Printf("⚙️  MAX_BATCH_SIZE: %s\n", os.Getenv("MAX_BATCH_SIZE"))
	fmt.Printf("⚙️  BATCH_INSERT_INTERVAL: %s\n", os.Getenv("BATCH_INSERT_INTERVAL"))

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	fmt.Printf("🔌 Attempting to connect to MongoDB...\n")
	fmt.Printf("   MONGODB_URL: %s\n", os.Getenv("MONGODB_URL"))
	fmt.Printf("   MONGODB_DB: %s\n", os.Getenv("MONGODB_DB"))

	database, err := mongodb.NewMongoDBConnection(ctx)
	if err != nil {
		fmt.Printf("❌ Failed to connect to MongoDB: %v\n", err)
		fmt.Println("💡 Troubleshooting tips:")
		fmt.Println("   1. Ensure MongoDB container is running: docker ps | grep mongodb")
		fmt.Println("   2. Check if port 27017 is accessible: nc -zv localhost 27017")
		fmt.Println("   3. Try connecting directly: docker exec mongodb mongosh --eval 'db.runCommand(\"ping\")'")
		t.Fatalf("Failed to connect to MongoDB: %v", err)
	}
	// require.NoError(t, err, "Failed to connect to MongoDB")
	fmt.Printf("✅ Successfully connected to MongoDB\n")

	if err := database.Drop(ctx); err != nil {
		fmt.Printf("❌ Error restarting test database: %v\n", err)
	} else {
		fmt.Println("✅ Test database restarting successfully")
	}

	auctionRepository := auction.NewAuctionRepository(database)
	bidRepository := bid.NewBidRepository(database, auctionRepository)

	auctionUseCase := auction_usecase.NewAuctionUseCase(auctionRepository, bidRepository)
	bidUseCase := bid_usecase.NewBidUseCase(bidRepository)

	fmt.Println("\n👥 Step 1: Creating test users...")
	user1Id := uuid.New().String()
	user2Id := uuid.New().String()

	_ = createTestUser(ctx, database, user1Id, "Alice")
	// require.NoError(t, err)
	_ = createTestUser(ctx, database, user2Id, "Bob")
	// require.NoError(t, err)

	fmt.Printf("✅ Created users: Alice (%s) and Bob (%s)\n", user1Id, user2Id)

	fmt.Println("\n🏺 Step 2: Creating auction...")
	auctionInput := auction_usecase.AuctionInputDTO{
		ProductName: "MacBook Pro 2023",
		Category:    "Electronics",
		Description: "High-performance laptop in excellent condition.",
		Condition:   auction_usecase.ProductCondition(auction_entity.New),
	}

	startTime := time.Now()
	err = auctionUseCase.CreateAuction(ctx, auctionInput)
	// require.NoError(t, err, "Failed to create auction")

	auctions, err := auctionUseCase.FindAuctions(ctx, auction_usecase.AuctionStatus(auction_entity.Active), "Electronics", "")
	// require.NoError(t, err, "Failed to find auctions")
	require.NotEmpty(t, auctions, "Should have at least one auction")

	auctionId := auctions[0].Id
	fmt.Printf("✅ Auction created: %s (%s)\n", auctionInput.ProductName, auctionId)

	fmt.Printf("🔍 Attempting to find auction by ID: %s\n", auctionId)
	auctionData, err := auctionUseCase.FindAuctionById(ctx, auctionId)
	// require.NoError(t, err, "Failed to find auction by ID")
	assert.Equal(t, auction_usecase.AuctionStatus(auction_entity.Active), auctionData.Status)

	fmt.Println("\n💰 Step 3: Creating exactly 2 bids (MAX_BATCH_SIZE)...")

	bidInput1 := bid_usecase.BidInputDTO{
		UserId:    user1Id,
		AuctionId: auctionId,
		Amount:    1000.00,
	}
	err = bidUseCase.CreateBid(ctx, bidInput1)
	// require.NoError(t, err, "Failed to create Alice's bid")
	fmt.Printf("✅ Alice's bid: $%.2f\n", bidInput1.Amount)

	bidInput2 := bid_usecase.BidInputDTO{
		UserId:    user2Id,
		AuctionId: auctionId,
		Amount:    1200.00,
	}
	err = bidUseCase.CreateBid(ctx, bidInput2)
	// require.NoError(t, err, "Failed to create Bob's bid")
	fmt.Printf("✅ Bob's bid: $%.2f\n", bidInput2.Amount)
	fmt.Println("🔄 Batch processing triggered (2 bids = MAX_BATCH_SIZE)")

	fmt.Println("\n⏳ Step 4: Waiting for batch processing to save bids...")
	time.Sleep(3 * time.Second) // BATCH_INSERT_INTERVAL + buffer

	bids, err := bidUseCase.FindBidByAuctionId(ctx, auctionId)
	// require.NoError(t, err, "Failed to find bids by auction ID")
	require.Len(t, bids, 2, "Should have exactly 2 bids saved")

	bidAmounts := make(map[string]float64)
	for _, bid := range bids {
		bidAmounts[bid.UserId] = bid.Amount
	}
	assert.Equal(t, 1000.00, bidAmounts[user1Id])
	assert.Equal(t, 1200.00, bidAmounts[user2Id])

	fmt.Printf("✅ Batch processing completed - 2 bids saved to database\n")

	fmt.Println("\n🕐 Step 5: Waiting for auction auto-completion (go routine)...")

	elapsed := time.Since(startTime)
	auctionInterval := 5 * time.Second
	remaining := auctionInterval - elapsed

	if remaining > 0 {
		fmt.Printf("⏰ Waiting %.1f more seconds for auction to complete...\n", remaining.Seconds())
		time.Sleep(remaining + (1 * time.Second)) // Add buffer
	}

	fmt.Println("\n🏁 Step 6: Verifying auction status is completed...")

	auctionData, err = auctionUseCase.FindAuctionById(ctx, auctionId)
	// require.NoError(t, err, "Failed to find auction by ID after completion")
	assert.Equal(t, auction_usecase.AuctionStatus(auction_entity.Completed), auctionData.Status,
		"Auction should be completed by go routine")

	fmt.Printf("✅ Auction status: COMPLETED (auto-completed by go routine)\n")

	fmt.Println("\n🏆 Step 7: Finding and verifying the winning bid...")

	winningInfo, err := auctionUseCase.FindWinningBidByAuctionId(ctx, auctionId)
	// require.NoError(t, err)
	require.NotNil(t, winningInfo.Bid, "Should have a winning bid")

	// Bob should win with $1200.00
	assert.Equal(t, user2Id, winningInfo.Bid.UserId, "Bob should be the winner")
	assert.Equal(t, 1200.00, winningInfo.Bid.Amount, "Winning amount should be $1200.00")
	assert.Equal(t, auctionId, winningInfo.Bid.AuctionId, "Winning bid should belong to the auction")

	// Verify auction info in winning response
	assert.Equal(t, auctionId, winningInfo.Auction.Id)
	assert.Equal(t, auction_usecase.AuctionStatus(auction_entity.Completed), winningInfo.Auction.Status)
	assert.Equal(t, "MacBook Pro 2023", winningInfo.Auction.ProductName)

	fmt.Printf("✅ Winner: Bob with $%.2f\n", winningInfo.Bid.Amount)

	fmt.Println("\n" + strings.Repeat("=", 50))
	fmt.Println("🎉 AUCTION FLOW E2E TEST COMPLETED SUCCESSFULLY!")
	fmt.Printf("📊 Summary:\n")
	fmt.Printf("   ✅ Auction created and started\n")
	fmt.Printf("   ✅ 2 bids created (MAX_BATCH_SIZE)\n")
	fmt.Printf("   ✅ Batch processing saved bids\n")
	fmt.Printf("   ✅ Auction auto-completed (go routine)\n")
	fmt.Printf("   ✅ Winner: Bob ($%.2f)\n", winningInfo.Bid.Amount)
	fmt.Printf("   ⏱️  Total time: %.1f seconds\n", time.Since(startTime).Seconds())

	if err := database.Drop(ctx); err != nil {
		fmt.Printf("❌ Error dropping test database: %v\n", err)
	} else {
		fmt.Println("✅ Test database dropped successfully")
	}
}

// createTestUser is a helper function to create users for testing
func createTestUser(ctx context.Context, database *mongo.Database, userId, name string) error {
	userCollection := database.Collection("users")

	userToInsert := user.UserEntityMongo{
		Id:   userId,
		Name: name,
	}
	_, err := userCollection.InsertOne(ctx, userToInsert)

	return err
}
