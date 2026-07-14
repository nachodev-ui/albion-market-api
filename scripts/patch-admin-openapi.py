from pathlib import Path

OPENAPI_PATH = Path("openapi/openapi.yaml")
BILLING_MARKER = "  /api/v1/billing/checkout:\n"
ADMIN_START = "  /api/v1/admin/session:\n"
ADMIN_TAG = (
    "  - name: Admin\n"
    "    description: Administración autenticada y auditada de usuarios y acceso Pro.\n"
)

ADMIN_BLOCK = """  /api/v1/admin/session:
    get:
      tags: [Admin]
      operationId: getAdminSession
      security:
        - userBearer: []
      summary: Comprobar la autorización administrativa del usuario autenticado
      responses:
        '200':
          description: Sesión administrativa activa.
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
          description: Cuenta autenticada sin autorización administrativa.
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/ErrorResponse'
        '405':
          $ref: '#/components/responses/MethodNotAllowedGet'

  /api/v1/admin/users:
    get:
      tags: [Admin]
      operationId: searchAdminUsers
      security:
        - userBearer: []
      summary: Buscar usuarios para administración
      parameters:
        - name: q
          in: query
          schema:
            type: string
            maxLength: 200
        - name: limit
          in: query
          schema:
            type: integer
            minimum: 1
            maximum: 100
            default: 50
      responses:
        '200':
          description: Usuarios coincidentes y su acceso efectivo.
          content:
            application/json:
              schema:
                type: object
                required: [users]
                properties:
                  users:
                    type: array
                    items:
                      type: object
                      additionalProperties: true
        '401':
          description: Access token ausente o inválido.
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/ErrorResponse'
        '403':
          description: Cuenta sin autorización administrativa.
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/ErrorResponse'
        '405':
          $ref: '#/components/responses/MethodNotAllowedGet'

  /api/v1/admin/users/{userId}:
    get:
      tags: [Admin]
      operationId: getAdminUser
      security:
        - userBearer: []
      summary: Consultar el detalle administrativo de un usuario
      parameters:
        - name: userId
          in: path
          required: true
          schema:
            type: string
            format: uuid
      responses:
        '200':
          description: Usuario, suscripciones y entitlements efectivos.
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
          description: Cuenta sin autorización administrativa.
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/ErrorResponse'
        '404':
          description: Usuario no encontrado.
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/ErrorResponse'
        '405':
          $ref: '#/components/responses/MethodNotAllowedGet'

  /api/v1/admin/users/{userId}/grant-pro:
    post:
      tags: [Admin]
      operationId: grantAdminPro
      security:
        - userBearer: []
      summary: Conceder acceso Pro manual y auditado
      parameters:
        - name: userId
          in: path
          required: true
          schema:
            type: string
            format: uuid
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              additionalProperties: false
              required: [durationDays, reason, confirmation]
              properties:
                durationDays:
                  type: integer
                  minimum: 1
                  maximum: 365
                reason:
                  type: string
                  minLength: 3
                  maxLength: 500
                confirmation:
                  type: string
                  const: GRANT PRO
      responses:
        '200':
          description: Resultado idempotente del grant.
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
          description: Cuenta sin autorización administrativa.
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/ErrorResponse'
        '404':
          description: Usuario no encontrado.
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/ErrorResponse'
        '405':
          $ref: '#/components/responses/MethodNotAllowedPost'

  /api/v1/admin/users/{userId}/revoke-pro:
    post:
      tags: [Admin]
      operationId: revokeAdminPro
      security:
        - userBearer: []
      summary: Retirar el grant Pro manual y registrar auditoría
      parameters:
        - name: userId
          in: path
          required: true
          schema:
            type: string
            format: uuid
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              additionalProperties: false
              required: [reason, confirmation]
              properties:
                reason:
                  type: string
                  minLength: 3
                  maxLength: 500
                confirmation:
                  type: string
                  const: REVOKE PRO
      responses:
        '200':
          description: Resultado idempotente de la revocación.
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
          description: Cuenta sin autorización administrativa.
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/ErrorResponse'
        '404':
          description: Usuario no encontrado.
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/ErrorResponse'
        '405':
          $ref: '#/components/responses/MethodNotAllowedPost'

  /api/v1/admin/audit-events:
    get:
      tags: [Admin]
      operationId: listAdminAuditEvents
      security:
        - userBearer: []
      summary: Consultar el historial administrativo append-only
      parameters:
        - name: limit
          in: query
          schema:
            type: integer
            minimum: 1
            maximum: 500
            default: 100
      responses:
        '200':
          description: Eventos administrativos más recientes.
          content:
            application/json:
              schema:
                type: object
                required: [events]
                properties:
                  events:
                    type: array
                    items:
                      type: object
                      additionalProperties: true
        '401':
          description: Access token ausente o inválido.
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/ErrorResponse'
        '403':
          description: Cuenta sin autorización administrativa.
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/ErrorResponse'
        '405':
          $ref: '#/components/responses/MethodNotAllowedGet'

"""


def main() -> None:
    text = OPENAPI_PATH.read_text(encoding="utf-8")

    start = text.find(ADMIN_START)
    if start >= 0:
        end = text.find(BILLING_MARKER, start)
        if end < 0:
            raise RuntimeError("billing marker not found after admin block")
        text = text[:start] + text[end:]

    if ADMIN_TAG not in text:
        billing_tag = (
            "  - name: Billing\n"
            "    description: Checkout, portal de cliente y sincronización de suscripciones.\n"
        )
        if billing_tag not in text:
            raise RuntimeError("billing tag marker not found")
        text = text.replace(billing_tag, billing_tag + ADMIN_TAG, 1)

    if BILLING_MARKER not in text:
        raise RuntimeError("billing path marker not found")
    text = text.replace(BILLING_MARKER, ADMIN_BLOCK + BILLING_MARKER, 1)
    OPENAPI_PATH.write_text(text, encoding="utf-8")


if __name__ == "__main__":
    main()
