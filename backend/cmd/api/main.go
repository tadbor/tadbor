// Tadbor HTTP API entrypoint. Wires the shared MongoDB connection into each
// internal service and mounts every service's routes on one gin engine.
//
// Config comes from the environment: MONGO_URI, MONGO_DB_NAME (default
// "tadbor") and PORT (default 8080).
package main

import (
	"log"
	"os"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"

	"tadbor/backend/internal/content"
	"tadbor/backend/internal/embedding"
	"tadbor/backend/internal/platform"
	"tadbor/backend/internal/quran"
	"tadbor/backend/internal/recitation"
	"tadbor/backend/internal/retrieval"
	"tadbor/backend/internal/review"
)

func main() {
	// .env is optional — docker-compose and the Makefile pass MONGO_URI
	// directly — so a missing file must not be fatal.
	if err := godotenv.Load(); err != nil && !os.IsNotExist(err) {
		log.Printf("godotenv: %v", err)
	}

	uri := os.Getenv("MONGO_URI")
	if uri == "" {
		log.Fatal("MONGO_URI is not set")
	}

	db, err := platform.NewMongoConnection()
	if err != nil {
		log.Fatalf("mongo: %v", err)
	}
	defer platform.CloseMongoConnection(db)

	router := gin.Default()
	router.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})

	quran.RegisterRoutes(router, quran.NewService(db))
	content.RegisterRoutes(router, content.NewService(db))
	recitation.RegisterRoutes(router, recitation.NewService(db))

	// The embedding provider is optional. Without credentials retrieval still
	// serves every ayah that has exact-mapped commentary; it just reports gaps
	// as INSUFFICIENT_EVIDENCE instead of falling back to thematic search.
	var embedder embedding.Embedder
	if url, key := os.Getenv("EMBEDDING_API_URL"), os.Getenv("EMBEDDING_API_KEY"); url != "" && key != "" {
		c, err := embedding.NewClient(url, key)
		if err != nil {
			log.Fatalf("embedding client: %v", err)
		}
		embedder = c
	} else {
		log.Printf("EMBEDDING_API_URL/EMBEDDING_API_KEY not set: retrieval will use exact verse mapping only")
	}

	// Internal services are mounted under a group so reviewer auth can gate
	// them as a unit once platform.ReviewerAuthMiddleware stops being a stub.
	internal := router.Group("", platform.ReviewerAuthMiddleware())
	retrieval.RegisterRoutes(internal, retrieval.NewService(db, embedder))
	review.RegisterRoutes(internal, review.NewService(db))

	port := 8080
	if v := os.Getenv("PORT"); v != "" {
		port, err = strconv.Atoi(v)
		if err != nil {
			log.Fatalf("PORT: %v", err)
		}
	}

	log.Printf("tadbor api listening on :%d", port)
	if err := router.Run(":" + strconv.Itoa(port)); err != nil {
		log.Fatal(err)
	}
}
