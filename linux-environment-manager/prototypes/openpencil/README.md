# Protótipo OpenPencil do LEM

Este diretório guarda o protótipo visual completo do dashboard, criado com base na interface atual.

- `lem-dashboard.fig`: arquivo editável no OpenPencil, com 14 telas/estados.
- `source/build.js`: construtor nativo do protótipo; cada elemento é um nó editável.
- `source/index.html` e `source/styles.css`: referência visual navegável, útil para revisar textos e estados.
- `preview/lem-dashboard.png`: imagem exportada para revisão rápida.
- `build.sh`: gera novamente o `.fig` e o PNG usando Docker, sem exigir Node no host.

## Abrir

1. Abra `https://app.openpencil.dev`.
2. Use **File → Open**.
3. Selecione `lem-dashboard.fig`.

O formato editável suportado pelo OpenPencil atualmente é `.fig`; `.pen` ainda não possui writer oficial.

## Regenerar

```bash
./prototypes/openpencil/build.sh
```

O protótipo é um artefato de design. Ele não deve ser tratado como implementação de produção.
