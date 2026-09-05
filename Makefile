.PHONY: start stop status infra-up infra-down obs-up obs-down backend frontend

start:
	powershell.exe -NoProfile -ExecutionPolicy Bypass -File scripts/dev-start.ps1

stop:
	powershell.exe -NoProfile -ExecutionPolicy Bypass -File scripts/dev-stop.ps1

status:
	powershell.exe -NoProfile -ExecutionPolicy Bypass -File scripts/dev-status.ps1

infra-up:
	powershell.exe -NoProfile -ExecutionPolicy Bypass -File scripts/dev-start.ps1 -InfraOnly

infra-down:
	docker compose stop

obs-up:
	docker compose -f docker-compose.observability.yml up -d

obs-down:
	docker compose -f docker-compose.observability.yml stop

backend:
	powershell.exe -NoProfile -ExecutionPolicy Bypass -File scripts/run-backend.ps1

frontend:
	powershell.exe -NoProfile -ExecutionPolicy Bypass -File scripts/run-frontend.ps1
