from pathlib import Path

p = Path('internal/handlers/playerprofile.go')
s = p.read_text()
s = s.replace(
'''func (h *AuthenticatedPlayerProfileHandler) Current(w http.ResponseWriter, r *http.Request) {
	h.serve(w, r, h.handler.Current)
}

func (h *AuthenticatedPlayerProfileHandler) Refresh(w http.ResponseWriter, r *http.Request) {
	h.serve(w, r, h.handler.Refresh)
}
''',
'''func (h *AuthenticatedPlayerProfileHandler) Search(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	h.handler.Search(w, r)
}

func (h *AuthenticatedPlayerProfileHandler) Current(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	h.serve(w, r, h.handler.Current)
}

func (h *AuthenticatedPlayerProfileHandler) Link(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		w.Header().Set("Allow", http.MethodPut)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	h.serve(w, r, h.handler.Current)
}

func (h *AuthenticatedPlayerProfileHandler) Unlink(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		w.Header().Set("Allow", http.MethodDelete)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	h.serve(w, r, h.handler.Current)
}

func (h *AuthenticatedPlayerProfileHandler) Refresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	h.serve(w, r, h.handler.Refresh)
}
''')
p.write_text(s)

p = Path('internal/server/router.go')
s = p.read_text()
s = s.replace(
'''if len(accountRoutes) > 0 && accountRoutes[0].PlayerProfileHandler != nil {
		mux.HandleFunc("/api/v1/albion/players/search", accountRoutes[0].PlayerProfileHandler.Search)
	}
''',
'''if len(accountRoutes) > 0 && accountRoutes[0].PlayerProfileHandler != nil {
		profileHandler := handlers.NewAuthenticatedPlayerProfileHandler(
			accountRoutes[0].PlayerProfileHandler,
			accountRoutes[0].Authenticator,
		)
		mux.HandleFunc("/api/v1/albion/players/search", profileHandler.Search)
	}
''')
s = s.replace(
'''mux.HandleFunc("/api/v1/me/albion-profile", profileHandler.Current)
			mux.HandleFunc("/api/v1/me/albion-profile/refresh", profileHandler.Refresh)''',
'''mux.HandleFunc("/api/v1/me/albion-profile", profileHandler.Current)
			mux.HandleFunc("/api/v1/me/albion-profile/link", profileHandler.Link)
			mux.HandleFunc("/api/v1/me/albion-profile/unlink", profileHandler.Unlink)
			mux.HandleFunc("/api/v1/me/albion-profile/refresh", profileHandler.Refresh)''')
p.write_text(s)

p = Path('internal/deployment/player_profile_contract_test.go')
s = p.read_text().replace(
'`/api/v1/me/albion-profile`,\n\t\t`/api/v1/me/albion-profile/refresh`,',
'`/api/v1/me/albion-profile`,\n\t\t`/api/v1/me/albion-profile/link`,\n\t\t`/api/v1/me/albion-profile/unlink`,\n\t\t`/api/v1/me/albion-profile/refresh`,')
p.write_text(s)

p = Path('openapi/openapi.yaml')
s = p.read_text()
if '  - name: PlayerProfile\n' not in s:
    s = s.replace(
        '  - name: Accounts\n    description: Perfil autenticado, suscripción y entitlements efectivos.\n',
        '  - name: Accounts\n    description: Perfil autenticado, suscripción y entitlements efectivos.\n'
        '  - name: PlayerProfile\n    description: Vinculación no verificada, resumen PvP y actividad pública de Albion.\n',
        1,
    )
start = s.index('  /api/v1/albion/players/search:\n')
end = s.index('  /api/v1/billing/checkout:\n', start)
block = '''  /api/v1/albion/players/search:
    get:
      tags: [PlayerProfile]
      operationId: searchAlbionPlayers
      security: []
      summary: Buscar personajes públicos de Albion Online
      parameters:
        - name: server
          in: query
          required: true
          schema:
            type: string
            enum: [americas, europe, asia]
        - name: name
          in: query
          required: true
          schema:
            type: string
            minLength: 3
            maxLength: 32
      responses:
        '200':
          description: Coincidencias públicas encontradas.
          content:
            application/json:
              schema:
                type: object
                required: [players]
                properties:
                  players:
                    type: array
                    items:
                      type: object
                      additionalProperties: true
        '400':
          $ref: '#/components/responses/BadRequest'
        '405':
          $ref: '#/components/responses/MethodNotAllowedGet'
        '429':
          $ref: '#/components/responses/RateLimited'
        '502':
          description: El proveedor público de Albion no respondió correctamente.
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/ErrorResponse'

  /api/v1/me/albion-profile:
    get:
      tags: [PlayerProfile]
      operationId: getMyAlbionProfile
      security:
        - userBearer: []
      summary: Obtener el personaje vinculado, su resumen PvP y actividad reciente
      responses:
        '200':
          description: Perfil vinculado y caché actual.
          content:
            application/json:
              schema:
                type: object
                additionalProperties: true
        '401':
          description: Access token ausente o inválido.
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/ErrorResponse'
        '403':
          description: Token sin el scope requerido.
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/ErrorResponse'
        '404':
          description: La cuenta todavía no tiene un personaje vinculado.
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/ErrorResponse'
        '405':
          $ref: '#/components/responses/MethodNotAllowedGet'

  /api/v1/me/albion-profile/link:
    put:
      tags: [PlayerProfile]
      operationId: linkMyAlbionProfile
      security:
        - userBearer: []
      summary: Vincular un personaje público a la cuenta autenticada
      description: La vinculación queda marcada como no verificada y no demuestra propiedad del personaje.
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              additionalProperties: false
              required: [server, playerId]
              properties:
                server:
                  type: string
                  enum: [americas, europe, asia]
                playerId:
                  type: string
                  minLength: 1
                  maxLength: 128
      responses:
        '200':
          description: Personaje vinculado y caché inicial guardada.
          content:
            application/json:
              schema:
                type: object
                additionalProperties: true
        '400':
          $ref: '#/components/responses/BadRequest'
        '401':
          description: Access token ausente o inválido.
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/ErrorResponse'
        '403':
          description: Token sin el scope requerido.
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/ErrorResponse'
        '405':
          description: Método no permitido.
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/ErrorResponse'
        '502':
          description: El proveedor público de Albion no respondió correctamente.
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/ErrorResponse'

  /api/v1/me/albion-profile/unlink:
    delete:
      tags: [PlayerProfile]
      operationId: unlinkMyAlbionProfile
      security:
        - userBearer: []
      summary: Desvincular el personaje y eliminar su caché asociada
      responses:
        '204':
          description: Perfil desvinculado.
        '401':
          description: Access token ausente o inválido.
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/ErrorResponse'
        '403':
          description: Token sin el scope requerido.
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/ErrorResponse'
        '404':
          description: La cuenta no tiene un personaje vinculado.
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/ErrorResponse'
        '405':
          description: Método no permitido.
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/ErrorResponse'

  /api/v1/me/albion-profile/refresh:
    post:
      tags: [PlayerProfile]
      operationId: refreshMyAlbionProfile
      security:
        - userBearer: []
      summary: Actualizar estadísticas y actividad reciente respetando el cooldown
      responses:
        '200':
          description: Perfil actualizado.
          content:
            application/json:
              schema:
                type: object
                additionalProperties: true
        '401':
          description: Access token ausente o inválido.
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/ErrorResponse'
        '403':
          description: Token sin el scope requerido.
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/ErrorResponse'
        '404':
          description: La cuenta no tiene un personaje vinculado.
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/ErrorResponse'
        '405':
          $ref: '#/components/responses/MethodNotAllowedPost'
        '429':
          description: La actualización manual todavía está en cooldown.
          headers:
            Retry-After:
              schema:
                type: integer
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/ErrorResponse'
        '502':
          description: El proveedor público de Albion no respondió correctamente; se conserva el caché anterior.
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/ErrorResponse'

'''
s = s[:start] + block + s[end:]
p.write_text(s)
