create table if not exists project_memberships (
  operator_id uuid not null references operators(id),
  organization_id uuid not null references organizations(id),
  project_id uuid not null references projects(id),
  role text not null check (role in ('owner', 'admin', 'member')),
  created_at timestamptz not null,
  primary key (operator_id, project_id)
);

create index if not exists project_memberships_project_operator_idx
  on project_memberships(project_id, operator_id);

insert into project_memberships (
  operator_id,
  organization_id,
  project_id,
  role,
  created_at
)
select
  oo.operator_id,
  p.organization_id,
  p.id,
  oo.role,
  now()
from operator_organizations oo
join projects p on p.organization_id = oo.organization_id
on conflict (operator_id, project_id) do nothing;
