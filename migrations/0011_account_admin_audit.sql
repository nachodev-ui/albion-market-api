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

create or replace function reject_account_admin_audit_mutation()
returns trigger
language plpgsql
as $$
begin
    raise exception 'account_admin_audit_events is append-only';
end;
$$;

drop trigger if exists account_admin_audit_append_only on account_admin_audit_events;
create trigger account_admin_audit_append_only
before update or delete on account_admin_audit_events
for each row execute function reject_account_admin_audit_mutation();

update app_schema_state
set version = greatest(version, 11), updated_at = now()
where singleton = true;
