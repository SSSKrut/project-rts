#!/bin/bash

# Проверка, что bat установлен
if ! command -v bat &> /dev/null; then
    echo "Ошибка: bat не установлен. Пожалуйста, установите bat."
    exit 1
fi

# Если аргумент не передан, используем текущий каталог
TARGET_DIR="${1:-.}"

# Проверка, что путь существует
if [ ! -d "$TARGET_DIR" ]; then
    echo "Ошибка: каталог '$TARGET_DIR' не существует."
    exit 1
fi

# Рекурсивно ищем все .go файлы и обрабатываем их
find "$TARGET_DIR" -type f -name "*.go" | while read -r file; do
    bat "$file" --no-paging --file-name="$file" --style=-changes
done
