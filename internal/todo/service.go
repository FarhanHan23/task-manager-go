package todo

import (
	"context"
	"time"
)

// Service berisi logika bisnis. Ia tidak tahu apa-apa soal HTTP maupun SQL.
type Service struct {
	repo Repository
	now  func() time.Time // bisa diganti di test agar waktu deterministik
}

func NewService(repo Repository) *Service {
	return &Service{repo: repo, now: time.Now}
}

func (s *Service) Create(ctx context.Context, in CreateInput) (Todo, error) {
	in.Normalize()
	if err := in.Validate(); err != nil {
		return Todo{}, err
	}
	now := s.now().UTC()
	t := Todo{
		Title:       in.Title,
		Description: in.Description,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := s.repo.Create(ctx, &t); err != nil {
		return Todo{}, err
	}
	return t, nil
}

func (s *Service) Get(ctx context.Context, id int64) (Todo, error) {
	return s.repo.GetByID(ctx, id)
}

func (s *Service) List(ctx context.Context, f ListFilter) ([]Todo, error) {
	f.Normalize()
	return s.repo.List(ctx, f)
}

func (s *Service) Update(ctx context.Context, id int64, in UpdateInput) (Todo, error) {
	in.Normalize()
	if err := in.Validate(); err != nil {
		return Todo{}, err
	}
	t, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return Todo{}, err
	}
	in.Apply(&t)
	t.UpdatedAt = s.now().UTC()
	if err := s.repo.Update(ctx, t); err != nil {
		return Todo{}, err
	}
	return t, nil
}

func (s *Service) Delete(ctx context.Context, id int64) error {
	return s.repo.Delete(ctx, id)
}
