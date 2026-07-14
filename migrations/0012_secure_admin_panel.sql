create table if not exists app_admins (
    id uuid primary key default gen_random_uuid(),
    user_id uuid not null references app_users(id) on delete restrict,
    active boolean not null default true,
    created_by text not null,
    reason text not null,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now(),
    disabled_at timestamptz,
    constraint app_admins_user_unique unique (user_id),
    constraint app_admins_created_by_length
        check (char_length(created_by) between 3 and 200),
    constraint app_admins_reason_length
        check (char_length(reason) between 3 and 500),
    constraint app_admins_disabled_state
        check (
            (active = true and disabled_at is null)
            or (active = false and disabled_at is not null)
        )
);

create index if not exists app_admins_active_created_idx
    on app_admins (active, created_at desc);

update app_schema_state
set version = greatest(version, 12), updated_at = now()
where singleton = true;
