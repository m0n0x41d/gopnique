alter table operator_organizations
  drop constraint if exists operator_organizations_role_check;

alter table operator_organizations
  add constraint operator_organizations_role_check
  check (role in ('owner', 'member'));
