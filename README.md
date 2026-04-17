# Backend Setup (Gin + Gorm + PostgreSQL)

## Menjalankan server

1. Salin `.env.example` menjadi `.env`.
2. Isi `ADMIN_EMAIL`, `ADMIN_PASSWORD`, dan `JWT_SECRET`.
3. Saat startup, backend akan memastikan akun admin ada di tabel `users` (password disimpan sebagai `password_hash` bcrypt).
2. Pastikan PostgreSQL aktif dan database tersedia.
3. Jalankan:

```bash
go run main.go
```

Server default berjalan di `http://localhost:8080`.

## Seed data otomatis

- Saat startup pertama, backend otomatis mengisi data dummy User, Category, dan Article jika tabel artikel masih kosong.
- Seed ada di `internal/seed/seed.go`.

## Endpoint utama

- `GET /api/articles` -> daftar artikel terbaru (maks 12)
- `GET /api/articles/:slug` -> detail artikel berdasarkan slug

## Auth endpoint

- `POST /api/auth/login` -> login berbasis tabel `users` (`email` + `password_hash`), mengembalikan access token JWT
- `GET /api/auth/me` -> validasi token dan ambil profile/role dari server

## Endpoint admin (JWT role admin)

- `POST /api/articles`
- `PATCH /api/articles/:slug`
- `DELETE /api/articles/:slug` (soft delete via `deleted_at`)
- `POST /api/uploads`
- `DELETE /api/uploads`

## CORS development

- Backend mengizinkan origin frontend lokal:
	- `http://localhost:5173`
	- `http://127.0.0.1:5173`

## Catatan skema database

- Definisi SQL ada di `../database/schema.sql`
- Model Gorm ada di `internal/models/models.go`
- `AutoMigrate` aktif saat server startup
