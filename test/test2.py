#!/usr/bin/env python3
"""
간단한 OpenAI API 예제
"""

import os

from openai import OpenAI


def main():
    # OpenAI 클라이언트 초기화
    # API 키는 환경 변수 OPENAI_API_KEY에서 자동으로 읽어집니다
    client = OpenAI(
        api_key="om-ni-f2ad646b7961b44bd002edda6812eb84a295291c4cb095486e825d4dc215d848",
        base_url="http://localhost:8080/opencode-go/v1",
    )

    # 간단한 채팅 완성 요청
    message = client.chat.completions.create(
        model="deepseek-v4-flash",
        messages=[
            {
                "role": "system",
                "content": "You are a helpful assistant.",
            },
            {
                "role": "user",
                "content": "안녕하세요! 오늘 기분은 어떻게 되나요?",
            },
        ],
        temperature=1,
    )

    # 응답 출력
    print("Assistant:", message.choices[0].message.content)


if __name__ == "__main__":
    main()
