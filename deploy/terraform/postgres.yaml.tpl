apiVersion: 1

datasources:
  - name: postgres
    uid: postgres
    type: postgres
    access: proxy
    url: postgresql:5432
    user: postgres
    secureJsonData:
      password: "${postgres_password}"
    jsonData:
      database: postgres
      sslmode: disable
      encrypt: disable
    isDefault: false
    editable: false
