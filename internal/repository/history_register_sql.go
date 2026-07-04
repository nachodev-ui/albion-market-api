package repository

const registerHistoryIngestRequestSQL = `
	insert into market_history_ingest_requests (
		request_id,
		request_sha256,
		server,
		accepted_entries,
		accepted_buckets,
		status
	)
	values ($1, $2, $3, $4, $5, 'processing')
	on conflict (request_id) do nothing
`

const completeHistoryIngestRequestSQL = `
	update market_history_ingest_requests
	set
		status = 'completed',
		history_rows_touched = $2,
		completed_at = now()
	where request_id = $1
`
