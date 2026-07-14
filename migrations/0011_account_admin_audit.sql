create table if not exists account_admin_audit_events (
    id uuid primary key default gen_random_uuid(),
    user_id uuid not null references app_users(id) on delete restrict,
    actor text not null,
    action text not null,
    reason text not null,
    source text not null default 'account-admin',
    before_state jsonb not null,
    after_state jsonb not null,
    created_at timestamptz not null default now(),
    constraint account_admin_audit_actor_length
        check (char_length(actor) between 3 and 200),
    constraint account_admin_audit_action_allowed
        check (action in ('grant_pro', 'revoke_pro')),
    constraint account_admin_audit_reason_length
        check (char_length(reason) between 3 and 500),
    constraint account_admin_audit_source_allowed
        check (source = 'account-admin')
);

create index if not exists account_admin_audit_user_created_idx
    on account_admin_audit_events (user_id, created_at desc);

create index if not exists account_admin_audit_action_created_idx
    on account_admin_audit_events (action, created_at desc);

create or replace rule account_admin_audit_no_update as
on update to account_admin_audit_events
do instead nothing;

create or replace rule account_admin_audit_no_delete as
on delete to account_admin_audit_events
do instead nothing;

update app_schema_state
set version = greatest(version, 11), updated_at = now()
where singleton = true;
