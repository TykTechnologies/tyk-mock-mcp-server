package store

import (
	"fmt"
	"sync"
	"time"

	"github.com/TykTechnologies/tyk-mock-mcp-server/models"
	"github.com/google/uuid"
)

type Store struct {
	users    map[string]*models.User
	posts    map[string]*models.Post
	products map[string]*models.Product
	orders   map[string]*models.Order
	mu       sync.RWMutex
}

func NewStore() *Store {
	s := &Store{
		users:    make(map[string]*models.User),
		posts:    make(map[string]*models.Post),
		products: make(map[string]*models.Product),
		orders:   make(map[string]*models.Order),
	}
	s.initMockData()
	return s
}

func (s *Store) initMockData() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.users["user-1"] = &models.User{
		ID:        "user-1",
		Name:      "John Doe",
		Email:     "john@example.com",
		Role:      "admin",
		Active:    true,
		CreatedAt: time.Now().Add(-24 * time.Hour),
	}
	s.users["user-2"] = &models.User{
		ID:        "user-2",
		Name:      "Jane Smith",
		Email:     "jane@example.com",
		Role:      "user",
		Active:    true,
		CreatedAt: time.Now().Add(-48 * time.Hour),
	}

	s.posts["post-1"] = &models.Post{
		ID:        "post-1",
		Title:     "Getting Started with MCP",
		Content:   "Model Context Protocol is a new standard for connecting AI systems...",
		Author:    "John Doe",
		Status:    "published",
		CreatedAt: time.Now().Add(-12 * time.Hour),
	}
	s.posts["post-2"] = &models.Post{
		ID:        "post-2",
		Title:     "Building APIs with Go",
		Content:   "Go is an excellent language for building high-performance APIs...",
		Author:    "Jane Smith",
		Status:    "draft",
		CreatedAt: time.Now().Add(-6 * time.Hour),
	}

	s.products["prod-1"] = &models.Product{
		ID:          "prod-1",
		Name:        "Wireless Headphones",
		Description: "High-quality noise-canceling wireless headphones",
		Price:       149.99,
		Stock:       50,
		Category:    "electronics",
	}
	s.products["prod-2"] = &models.Product{
		ID:          "prod-2",
		Name:        "Coffee Maker",
		Description: "Programmable coffee maker with thermal carafe",
		Price:       79.99,
		Stock:       30,
		Category:    "appliances",
	}
	s.products["prod-3"] = &models.Product{
		ID:          "prod-3",
		Name:        "Running Shoes",
		Description: "Comfortable running shoes with excellent support",
		Price:       89.99,
		Stock:       100,
		Category:    "sports",
	}
}

// User operations

func (s *Store) GetUsers(role string, active *bool) []models.User {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := []models.User{}
	for _, user := range s.users {
		if role != "" && user.Role != role {
			continue
		}
		if active != nil && user.Active != *active {
			continue
		}
		result = append(result, *user)
	}
	return result
}

func (s *Store) CreateUser(name, email, role string) *models.User {
	s.mu.Lock()
	defer s.mu.Unlock()

	if role == "" {
		role = "user"
	}

	user := &models.User{
		ID:        uuid.New().String(),
		Name:      name,
		Email:     email,
		Role:      role,
		Active:    true,
		CreatedAt: time.Now(),
	}

	s.users[user.ID] = user
	return user
}

func (s *Store) UpdateUser(id string, name, email, role *string, active *bool) (*models.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	user, exists := s.users[id]
	if !exists {
		return nil, fmt.Errorf("user not found: %s", id)
	}

	if name != nil {
		user.Name = *name
	}
	if email != nil {
		user.Email = *email
	}
	if role != nil {
		user.Role = *role
	}
	if active != nil {
		user.Active = *active
	}

	return user, nil
}

func (s *Store) DeleteUser(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.users[id]; !exists {
		return fmt.Errorf("user not found: %s", id)
	}

	delete(s.users, id)
	return nil
}

func (s *Store) GetAllUsers() []models.User {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := []models.User{}
	for _, user := range s.users {
		result = append(result, *user)
	}
	return result
}

// Post operations

func (s *Store) GetPosts(author, status string) []models.Post {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := []models.Post{}
	for _, post := range s.posts {
		if author != "" && post.Author != author {
			continue
		}
		if status != "" && post.Status != status {
			continue
		}
		result = append(result, *post)
	}
	return result
}

func (s *Store) CreatePost(title, content, author, status string) *models.Post {
	s.mu.Lock()
	defer s.mu.Unlock()

	if status == "" {
		status = "draft"
	}

	post := &models.Post{
		ID:        uuid.New().String(),
		Title:     title,
		Content:   content,
		Author:    author,
		Status:    status,
		CreatedAt: time.Now(),
	}

	s.posts[post.ID] = post
	return post
}

func (s *Store) GetAllPosts() []models.Post {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := []models.Post{}
	for _, post := range s.posts {
		result = append(result, *post)
	}
	return result
}

// Product operations

func (s *Store) GetProducts(category string, minPrice, maxPrice float64) []models.Product {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := []models.Product{}
	for _, product := range s.products {
		if category != "" && product.Category != category {
			continue
		}
		if minPrice > 0 && product.Price < minPrice {
			continue
		}
		if maxPrice > 0 && product.Price > maxPrice {
			continue
		}
		result = append(result, *product)
	}
	return result
}

func (s *Store) GetProduct(id string) (*models.Product, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	product, exists := s.products[id]
	if !exists {
		return nil, fmt.Errorf("product not found: %s", id)
	}
	return product, nil
}

func (s *Store) UpdateProductStock(id string, quantity int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	product, exists := s.products[id]
	if !exists {
		return fmt.Errorf("product not found: %s", id)
	}

	product.Stock -= quantity
	return nil
}

func (s *Store) GetAllProducts() []models.Product {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := []models.Product{}
	for _, product := range s.products {
		result = append(result, *product)
	}
	return result
}

// Order operations

func (s *Store) CreateOrder(productID string, quantity int, totalPrice float64) *models.Order {
	s.mu.Lock()
	defer s.mu.Unlock()

	order := &models.Order{
		ID:         uuid.New().String(),
		ProductID:  productID,
		Quantity:   quantity,
		TotalPrice: totalPrice,
		Status:     "pending",
		CreatedAt:  time.Now(),
	}

	s.orders[order.ID] = order
	return order
}

func (s *Store) GetAllOrders() []models.Order {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := []models.Order{}
	for _, order := range s.orders {
		result = append(result, *order)
	}
	return result
}

// Analytics operations

func (s *Store) GetUserCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.users)
}

func (s *Store) GetActiveUserCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	count := 0
	for _, user := range s.users {
		if user.Active {
			count++
		}
	}
	return count
}

func (s *Store) GetPostCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.posts)
}

func (s *Store) GetPostCountByStatus() map[string]int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make(map[string]int)
	for _, post := range s.posts {
		result[post.Status]++
	}
	return result
}

func (s *Store) GetOrderCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.orders)
}

func (s *Store) GetTotalRevenue() float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()

	total := 0.0
	for _, order := range s.orders {
		total += order.TotalPrice
	}
	return total
}
