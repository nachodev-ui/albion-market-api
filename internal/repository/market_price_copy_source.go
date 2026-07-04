package repository

import "github.com/jackc/pgx/v5"

var _ pgx.CopyFromSource = (*rawPriceCopySource)(nil)
