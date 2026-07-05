# Avisos de terceros

## Coeficientes HOG/SVM para detección de personas

Fragata incluye dentro del código los coeficientes del detector humano HOG/SVM predeterminado expuesto por OpenCV mediante `HOGDescriptor::getDefaultPeopleDetector()`.

Estos coeficientes se usan únicamente como datos numéricos por una implementación propia escrita en Go. Fragata no enlaza, carga ni distribuye binarios de OpenCV.

OpenCV 4.x se distribuye bajo la licencia Apache License 2.0. El texto de esa licencia puede consultarse en:

```text
https://www.apache.org/licenses/LICENSE-2.0
```

Copyright de OpenCV y sus contribuidores según corresponda al proyecto original.
